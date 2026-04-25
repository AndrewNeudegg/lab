package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/andrewneudegg/lab/pkg/id"
)

type MatrixConfig struct {
	Homeserver    string
	User          string
	Password      string
	AccessToken   string
	RoomID        string
	RoomAlias     string
	RoomName      string
	SyncTimeoutMS int
}

type Matrix struct {
	cfg     MatrixConfig
	client  *http.Client
	mu      sync.RWMutex
	token   string
	userID  string
	roomID  string
	nextTxn int64
}

func NewMatrix(cfg MatrixConfig) *Matrix {
	cfg.Homeserver = strings.TrimRight(cfg.Homeserver, "/")
	if cfg.SyncTimeoutMS == 0 {
		cfg.SyncTimeoutMS = 30000
	}
	if cfg.RoomName == "" {
		cfg.RoomName = "landing"
	}
	return &Matrix{cfg: cfg, client: http.DefaultClient, token: cfg.AccessToken, roomID: cfg.RoomID}
}

func (m *Matrix) Name() string { return "matrix" }

func (m *Matrix) RoomID() string {
	return m.getRoomID()
}

func (m *Matrix) Receive(ctx context.Context) (<-chan ChatMessage, error) {
	if err := m.ensureAuth(ctx); err != nil {
		return nil, err
	}
	if m.cfg.RoomAlias != "" && m.getRoomID() == "" {
		if err := m.joinRoom(ctx, m.cfg.RoomAlias); err != nil {
			return nil, err
		}
	}
	initial, err := m.sync(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	if err := m.processInvites(ctx, initial); err != nil {
		return nil, err
	}
	if m.getRoomID() == "" {
		m.resolveRoomFromSync(initial)
	}
	if m.getRoomID() == "" {
		if err := m.resolveJoinedRoomByState(ctx); err != nil {
			return nil, err
		}
	}
	if m.getRoomID() == "" {
		return nil, fmt.Errorf("matrix bot %s is not joined to room %q; invite it to the room or set MATRIX_ROOM_ID/MATRIX_ROOM_ALIAS", m.getUserID(), m.cfg.RoomName)
	}
	encrypted, err := m.roomEncrypted(ctx, m.getRoomID())
	if err != nil {
		return nil, err
	}
	if encrypted {
		return nil, fmt.Errorf("matrix room %q (%s) is encrypted; this adapter cannot decrypt E2EE rooms, use an unencrypted room or add an E2EE-capable Matrix client", m.cfg.RoomName, m.getRoomID())
	}
	ch := make(chan ChatMessage)
	go m.syncLoop(ctx, initial.NextBatch, ch)
	return ch, nil
}

func (m *Matrix) Send(ctx context.Context, msg OutboundMessage) error {
	roomID := msg.To
	if roomID == "" {
		roomID = m.getRoomID()
	}
	if roomID == "" {
		return fmt.Errorf("matrix room is not resolved yet")
	}
	body := map[string]any{"msgtype": "m.text", "body": msg.Content}
	var out map[string]any
	return m.doJSON(ctx, http.MethodPut, "/rooms/"+url.PathEscape(roomID)+"/send/m.room.message/"+url.PathEscape(m.txnID()), body, &out)
}

func (m *Matrix) ensureAuth(ctx context.Context) error {
	if m.cfg.Homeserver == "" {
		return fmt.Errorf("matrix homeserver is required")
	}
	if m.token != "" {
		return m.whoami(ctx)
	}
	if m.cfg.User == "" || m.cfg.Password == "" {
		return fmt.Errorf("matrix user/password or access token is required")
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
	}
	body := map[string]any{
		"type": "m.login.password",
		"identifier": map[string]any{
			"type": "m.id.user",
			"user": m.cfg.User,
		},
		"password": m.cfg.Password,
	}
	if err := m.doJSON(ctx, http.MethodPost, "/login", body, &resp); err != nil {
		return err
	}
	m.mu.Lock()
	m.token = resp.AccessToken
	m.userID = resp.UserID
	m.mu.Unlock()
	return nil
}

func (m *Matrix) whoami(ctx context.Context) error {
	var resp struct {
		UserID string `json:"user_id"`
	}
	if err := m.doJSON(ctx, http.MethodGet, "/account/whoami", nil, &resp); err != nil {
		return err
	}
	m.mu.Lock()
	m.userID = resp.UserID
	m.mu.Unlock()
	return nil
}

func (m *Matrix) joinRoom(ctx context.Context, roomIDOrAlias string) error {
	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := m.doJSON(ctx, http.MethodPost, "/join/"+url.PathEscape(roomIDOrAlias), map[string]any{}, &resp); err != nil {
		return err
	}
	m.setRoomID(resp.RoomID)
	return nil
}

func (m *Matrix) syncLoop(ctx context.Context, since string, ch chan<- ChatMessage) {
	defer close(ch)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		resp, err := m.sync(ctx, since, m.cfg.SyncTimeoutMS)
		if err != nil {
			sleep(ctx, 2*time.Second)
			continue
		}
		_ = m.processInvites(ctx, resp)
		m.resolveRoomFromSync(resp)
		since = resp.NextBatch
		roomID := m.getRoomID()
		if roomID == "" {
			continue
		}
		joined, ok := resp.Rooms.Join[roomID]
		if !ok {
			continue
		}
		for _, event := range joined.Timeline.Events {
			msg, ok := m.eventToMessage(event)
			if !ok {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- msg:
			}
		}
	}
}

func (m *Matrix) sync(ctx context.Context, since string, timeoutMS int) (matrixSyncResponse, error) {
	path := fmt.Sprintf("/sync?timeout=%d", timeoutMS)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}
	var resp matrixSyncResponse
	err := m.doJSON(ctx, http.MethodGet, path, nil, &resp)
	return resp, err
}

func (m *Matrix) resolveRoomFromSync(resp matrixSyncResponse) {
	if m.getRoomID() != "" {
		return
	}
	want := strings.TrimSpace(m.cfg.RoomName)
	if want == "" {
		return
	}
	for roomID, joined := range resp.Rooms.Join {
		for _, event := range joined.State.Events {
			if event.Type != "m.room.name" {
				continue
			}
			name, _ := event.Content["name"].(string)
			if strings.EqualFold(strings.TrimSpace(name), want) {
				m.setRoomID(roomID)
				return
			}
		}
	}
}

func (m *Matrix) resolveJoinedRoomByState(ctx context.Context) error {
	want := strings.TrimSpace(m.cfg.RoomName)
	if want == "" {
		return nil
	}
	var joined struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := m.doJSON(ctx, http.MethodGet, "/joined_rooms", nil, &joined); err != nil {
		return err
	}
	for _, roomID := range joined.JoinedRooms {
		var state []matrixEvent
		if err := m.doJSON(ctx, http.MethodGet, "/rooms/"+url.PathEscape(roomID)+"/state", nil, &state); err != nil {
			continue
		}
		for _, event := range state {
			if event.Type != "m.room.name" {
				continue
			}
			name, _ := event.Content["name"].(string)
			if strings.EqualFold(strings.TrimSpace(name), want) {
				m.setRoomID(roomID)
				return nil
			}
		}
	}
	return nil
}

func (m *Matrix) roomEncrypted(ctx context.Context, roomID string) (bool, error) {
	var state []matrixEvent
	if err := m.doJSON(ctx, http.MethodGet, "/rooms/"+url.PathEscape(roomID)+"/state", nil, &state); err != nil {
		return false, err
	}
	for _, event := range state {
		if event.Type == "m.room.encryption" {
			return true, nil
		}
	}
	return false, nil
}

func (m *Matrix) processInvites(ctx context.Context, resp matrixSyncResponse) error {
	want := strings.TrimSpace(m.cfg.RoomName)
	for roomID, invited := range resp.Rooms.Invite {
		if want != "" && !invited.matchesName(want) {
			continue
		}
		if err := m.joinRoom(ctx, roomID); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (m *Matrix) eventToMessage(event matrixEvent) (ChatMessage, bool) {
	if event.Type != "m.room.message" || event.Sender == m.getUserID() {
		return ChatMessage{}, false
	}
	msgType, _ := event.Content["msgtype"].(string)
	body, _ := event.Content["body"].(string)
	if msgType != "m.text" || strings.TrimSpace(body) == "" {
		return ChatMessage{}, false
	}
	eventTime := time.Now().UTC()
	if event.OriginServerTS > 0 {
		eventTime = time.UnixMilli(event.OriginServerTS).UTC()
	}
	return ChatMessage{ID: event.EventID, Time: eventTime, From: event.Sender, Content: body}, true
}

func (m *Matrix) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.cfg.Homeserver+"/_matrix/client/v3"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := m.getToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var matrixErr struct {
			ErrCode string `json:"errcode"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&matrixErr)
		if matrixErr.Error == "" {
			matrixErr.Error = resp.Status
		}
		return fmt.Errorf("matrix %s %s: %s", method, path, matrixErr.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (m *Matrix) getToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.token
}

func (m *Matrix) getUserID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userID
}

func (m *Matrix) getRoomID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.roomID
}

func (m *Matrix) setRoomID(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roomID = roomID
}

func (m *Matrix) txnID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextTxn++
	return id.New("matrix_txn")
}

type matrixSyncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join   map[string]matrixJoinedRoom  `json:"join"`
		Invite map[string]matrixInvitedRoom `json:"invite"`
	} `json:"rooms"`
}

type matrixJoinedRoom struct {
	State struct {
		Events []matrixEvent `json:"events"`
	} `json:"state"`
	Timeline struct {
		Events []matrixEvent `json:"events"`
	} `json:"timeline"`
}

type matrixInvitedRoom struct {
	InviteState struct {
		Events []matrixEvent `json:"events"`
	} `json:"invite_state"`
}

func (r matrixInvitedRoom) matchesName(want string) bool {
	if want == "" {
		return true
	}
	for _, event := range r.InviteState.Events {
		switch event.Type {
		case "m.room.name":
			name, _ := event.Content["name"].(string)
			if strings.EqualFold(strings.TrimSpace(name), want) {
				return true
			}
		case "m.room.canonical_alias":
			alias, _ := event.Content["alias"].(string)
			if strings.Contains(strings.ToLower(alias), strings.ToLower(want)) {
				return true
			}
		}
	}
	return false
}

type matrixEvent struct {
	Type           string         `json:"type"`
	Sender         string         `json:"sender"`
	EventID        string         `json:"event_id"`
	OriginServerTS int64          `json:"origin_server_ts"`
	Content        map[string]any `json:"content"`
}

func sleep(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
