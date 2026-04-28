package control

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTerminalSessionRunsInteractiveShellCommands(t *testing.T) {
	skipIfNoPTY(t)
	t.Setenv("SHELL", "/bin/sh")
	manager := newTerminalManager()
	session, err := manager.create("")
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer manager.close(session.id)

	events := session.subscribe()
	defer session.unsubscribe(events)

	if err := session.write("read name; printf 'hello:%s\\n' \"$name\"; exit\n"); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := session.write("web-terminal-ok\n"); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}

	output := collectTerminalOutput(t, events, 5*time.Second)
	if !strings.Contains(output, "hello:web-terminal-ok") {
		t.Fatalf("terminal output missing interactive command result: %q", output)
	}
}

func TestTerminalSessionPreservesAnsiSequences(t *testing.T) {
	skipIfNoPTY(t)
	t.Setenv("SHELL", "/bin/sh")
	manager := newTerminalManager()
	session, err := manager.create("")
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer manager.close(session.id)

	events := session.subscribe()
	defer session.unsubscribe(events)
	waitForTerminalOutput(t, events, "$", 5*time.Second)

	if err := session.write("printf '\\033[31mred\\033[0m\\n'\nexit\n"); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}

	output := collectTerminalOutput(t, events, 5*time.Second)
	if !strings.Contains(output, "\x1b[31m") || !strings.Contains(output, "red") {
		t.Fatalf("terminal output stripped ANSI colour sequences: %q", output)
	}
}

func TestTerminalSessionHandlesCursorKeyInput(t *testing.T) {
	skipIfNoPTY(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required for readline cursor-key coverage")
	}
	t.Setenv("SHELL", bash)
	manager := newTerminalManager()
	session, err := manager.create("")
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer manager.close(session.id)

	events := session.subscribe()
	defer session.unsubscribe(events)

	waitForTerminalOutput(t, events, "$", 5*time.Second)
	for _, input := range []string{"printf 'ab'", "\x1b[D", "c\nexit\n"} {
		if err := session.write(input); err != nil {
			t.Fatalf("write cursor-key input %q: %v", input, err)
		}
	}

	output := collectTerminalOutput(t, events, 5*time.Second)
	if !strings.Contains(output, "abc") {
		t.Fatalf("cursor-key editing was not applied: %q", output)
	}
}

func TestTerminalManagerReattachesPersistentTmuxSession(t *testing.T) {
	skipIfNoPTY(t)
	skipIfNoTmux(t)
	t.Setenv("SHELL", "/bin/sh")
	firstManager := newTerminalManager()
	session, err := firstManager.create("")
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer firstManager.close(session.id)
	defer killTerminalTmuxSession(tmuxSessionName(session.id))

	secondManager := newTerminalManager()
	reattached, ok, err := secondManager.getOrAttach(session.id, terminalSize{Cols: 90, Rows: 24})
	if err != nil {
		t.Fatalf("reattach terminal session: %v", err)
	}
	if !ok {
		t.Fatalf("persistent tmux session %s was not found", session.id)
	}
	events := reattached.subscribe()
	defer reattached.unsubscribe(events)

	if err := reattached.write("echo reattach-ok\nexit\n"); err != nil {
		t.Fatalf("write reattached input: %v", err)
	}

	output := collectTerminalOutput(t, events, 5*time.Second)
	if !strings.Contains(output, "reattach-ok") {
		t.Fatalf("reattached terminal output missing shell result: %q", output)
	}
}

func TestTerminalSessionResizesPTY(t *testing.T) {
	skipIfNoPTY(t)
	t.Setenv("SHELL", "/bin/sh")
	manager := newTerminalManager()
	session, err := manager.createWithSize("", terminalSize{Cols: 81, Rows: 19})
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer manager.close(session.id)

	if session.size.Cols != 81 || session.size.Rows != 19 {
		t.Fatalf("initial size = %+v, want 81x19", session.size)
	}
	if err := session.resize(terminalSize{Cols: 132, Rows: 41}); err != nil {
		t.Fatalf("resize terminal: %v", err)
	}

	events := session.subscribe()
	defer session.unsubscribe(events)
	if err := session.write("stty size\nexit\n"); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}

	output := collectTerminalOutput(t, events, 5*time.Second)
	if !strings.Contains(output, "41 132") {
		t.Fatalf("stty size did not see resized PTY: %q", output)
	}
}

func TestNormalizeTerminalSizeClampsUnsafeValues(t *testing.T) {
	size := normalizeTerminalSize(terminalSize{Cols: 9999, Rows: -1})
	if size.Cols != maxTerminalCols || size.Rows != defaultTerminalRows {
		t.Fatalf("normalized size = %+v", size)
	}
	size = normalizeTerminalSize(terminalSize{Cols: 1, Rows: 1})
	if size.Cols != 20 || size.Rows != 5 {
		t.Fatalf("minimum normalized size = %+v", size)
	}
}

func TestTerminalSessionSubscribeSinceReplaysOnlyNewEvents(t *testing.T) {
	session := &terminalSession{
		exitCode:  -1,
		listeners: make(map[chan terminalEvent]struct{}),
	}
	session.broadcast(terminalEvent{Type: "output", Data: "old"})
	session.broadcast(terminalEvent{Type: "output", Data: "new"})

	events := session.subscribeSince(1)
	defer session.unsubscribe(events)
	select {
	case event := <-events:
		if event.Data != "new" || event.Seq != 2 {
			t.Fatalf("first replayed event = %+v, want only seq 2 output", event)
		}
	default:
		t.Fatalf("subscribeSince did not replay newer output")
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected extra replayed event: %+v", event)
	default:
	}

	session.close(0)
	closedEvents := session.subscribeSince(2)
	event, ok := <-closedEvents
	if !ok || event.Type != "exit" || event.Seq != 3 {
		t.Fatalf("closed subscribe event = %+v ok=%v, want seq 3 exit", event, ok)
	}
	if _, ok := <-closedEvents; ok {
		t.Fatalf("closed subscribe channel stayed open")
	}
}

func TestTerminalStartupCommandUsesRunShellBootstrap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/usr/bin/env bash\nexec /bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := terminalCommand(dir, "/bin/sh")
	if cmd.Path != "./run.sh" || strings.Join(cmd.Args, " ") != "./run.sh shell" {
		t.Fatalf("terminal command = path %q args %q, want ./run.sh shell", cmd.Path, cmd.Args)
	}
	if got := terminalStartupCommand(dir, "/bin/sh"); got != "'./run.sh' shell" {
		t.Fatalf("tmux startup command = %q, want ./run.sh shell", got)
	}

	t.Setenv("HOMELAB_WEB_TERMINAL_SKIP_RUN_SH_SHELL", "1")
	if got := terminalStartupCommand(dir, "/bin/sh"); got == "'./run.sh' shell" {
		t.Fatalf("startup command used run.sh despite skip env")
	}
}

func TestTerminalProbeResponseDetection(t *testing.T) {
	probe := "\x1b[?1;2c\x1b[>0;276;0c\x1b]10;rgb:ffff/ffff/ffff\x1b\\\x1b]11;rgb:0000/0000/0000\a"
	if !terminalProbeResponse(probe) {
		t.Fatalf("probe response was not detected")
	}
	if terminalProbeResponse("\x1b[D") {
		t.Fatalf("cursor key was misclassified as a probe response")
	}
	if terminalProbeResponse("printf ok\n") {
		t.Fatalf("shell input was misclassified as a probe response")
	}
}

func TestTerminalFilterProbeResponsesStripsMixedAndSplitInput(t *testing.T) {
	filtered, pending := terminalFilterProbeResponses("", "printf ok\n\x1b[?1;2c\x1b[>0;276;0c")
	if filtered != "printf ok\n" || pending != "" {
		t.Fatalf("filtered mixed input = %q pending %q, want shell input only", filtered, pending)
	}

	filtered, pending = terminalFilterProbeResponses("", "\x1b[?")
	if filtered != "" || pending != "\x1b[?" {
		t.Fatalf("filtered partial probe = %q pending %q", filtered, pending)
	}
	filtered, pending = terminalFilterProbeResponses(pending, "1;2c0")
	if filtered != "0" || pending != "" {
		t.Fatalf("filtered completed partial probe = %q pending %q", filtered, pending)
	}

	filtered, pending = terminalFilterProbeResponses("\x1b[", "D")
	if filtered != "\x1b[D" || pending != "" {
		t.Fatalf("split cursor key was altered: %q pending %q", filtered, pending)
	}
}

func TestWebSocketHelpersRoundTripMaskedClientInput(t *testing.T) {
	var wire bytes.Buffer
	writeMaskedClientFrame(&wire, []byte("printf ok\n"))
	got, err := readWebSocketFrame(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "printf ok\n" {
		t.Fatalf("frame payload = %q", got)
	}
}

func TestWebSocketHelpersWriteBinaryFrames(t *testing.T) {
	var wire bytes.Buffer
	if err := writeWebSocketBinary(&wire, []byte("\x1b[32mok\x1b[0m")); err != nil {
		t.Fatal(err)
	}
	raw := wire.Bytes()
	if len(raw) < 2 || raw[0] != 0x82 {
		t.Fatalf("server frame header = % x", raw[:min(len(raw), 2)])
	}
	if string(raw[2:]) != "\x1b[32mok\x1b[0m" {
		t.Fatalf("server frame payload = %q", raw[2:])
	}
}

func TestTerminalWebSocketEndpointBridgesShell(t *testing.T) {
	testTerminalWebSocketEndpointBridgesShell(t, "/terminal/sessions", "/terminal/sessions/%s/ws")
}

func TestTerminalWebSocketEndpointAcceptsProxiedAPIPath(t *testing.T) {
	testTerminalWebSocketEndpointBridgesShell(t, "/api/terminal/sessions", "/api/terminal/sessions/%s/ws")
}

func TestTerminalOnlyServerExposesOnlyTerminalRoutes(t *testing.T) {
	server := Server{TerminalOnly: true}
	mux := http.NewServeMux()
	server.register(mux)

	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	tasks := httptest.NewRecorder()
	mux.ServeHTTP(tasks, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if tasks.Code != http.StatusNotFound {
		t.Fatalf("tasks status = %d, want 404", tasks.Code)
	}

	terminal := httptest.NewRecorder()
	mux.ServeHTTP(terminal, httptest.NewRequest(http.MethodGet, "/terminal/sessions/missing/events", nil))
	if terminal.Code != http.StatusNotFound {
		t.Fatalf("terminal status = %d, want handled terminal 404", terminal.Code)
	}
}

func testTerminalWebSocketEndpointBridgesShell(t *testing.T, createPath, socketPath string) {
	skipIfNoPTY(t)
	skipIfNoLoopback(t)
	t.Setenv("SHELL", "/bin/sh")
	server := Server{}
	mux := http.NewServeMux()
	server.register(mux)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	var created struct {
		ID string `json:"id"`
	}
	resp, err := http.Post(httpServer.URL+createPath, "application/json", strings.NewReader(`{"cols":90,"rows":25}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	defer server.terminals().close(created.ID)

	conn := dialTerminalWebSocket(t, httpServer.URL, fmt.Sprintf(socketPath, created.ID))
	defer conn.Close()
	if err := writeMaskedClientFrame(conn, []byte("echo ws-ok\n")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var output strings.Builder
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		payload, err := readWebSocketFrame(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if errorsIsEOF(err) {
				break
			}
			t.Fatal(err)
		}
		output.Write(payload)
		if strings.Contains(output.String(), "ws-ok") {
			return
		}
	}
	t.Fatalf("websocket output missing shell result: %q", output.String())
}

func collectTerminalOutput(t *testing.T, events <-chan terminalEvent, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	var output strings.Builder
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("terminal event stream closed before exit")
			}
			if event.Type == "output" {
				output.WriteString(event.Data)
			}
			if event.Type == "exit" {
				return output.String()
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal output: %q", output.String())
		}
	}
}

func waitForTerminalOutput(t *testing.T, events <-chan terminalEvent, needle string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	var output strings.Builder
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("terminal event stream closed before %q appeared", needle)
			}
			if event.Type == "output" {
				output.WriteString(event.Data)
				if strings.Contains(output.String(), needle) {
					return output.String()
				}
			}
			if event.Type == "exit" {
				t.Fatalf("terminal exited before %q appeared: %q", needle, output.String())
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q in terminal output: %q", needle, output.String())
		}
	}
}

func skipIfNoPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests are unix-only")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("HOMELAB_WEB_TERMINAL_SKIP_RUN_SH_SHELL", "1")
	skipIfNoTmux(t)
}

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for persistent terminal reattach coverage")
	}
	probeName := "homelab_test_tmux_probe"
	cmd := terminalTmuxCommand("new-session", "-d", "-s", probeName, "true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("tmux cannot create a session in this test environment: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = terminalTmuxCommand("kill-session", "-t", probeName).Run()
}

func skipIfNoLoopback(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this test environment: %v", err)
	}
	_ = ln.Close()
}

func dialTerminalWebSocket(t *testing.T, baseURL, path string) net.Conn {
	t.Helper()
	address := strings.TrimPrefix(baseURL, "http://")
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, address, key)
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("websocket status = %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &bufferedConn{Conn: conn, reader: reader}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func writeMaskedClientFrame(writer io.Writer, payload []byte) error {
	header := []byte{0x81}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, 0x80|byte(length))
	case length <= 0xffff:
		header = append(header, 0x80|126, byte(length>>8), byte(length))
	default:
		header = append(header, 0x80|127)
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], uint64(length))
		header = append(header, raw[:]...)
	}
	mask := []byte{1, 2, 3, 4}
	header = append(header, mask...)
	masked := append([]byte(nil), payload...)
	for i := range masked {
		masked[i] ^= mask[i%4]
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(masked)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func errorsIsEOF(err error) bool {
	return errors.Is(err, io.EOF) || strings.Contains(err.Error(), "use of closed network connection")
}
