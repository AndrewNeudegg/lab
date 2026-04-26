package control

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func (s *Server) streamTerminalWebSocket(rw http.ResponseWriter, req *http.Request, session *terminalSession) {
	conn, err := acceptWebSocket(rw, req)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffered := bufio.NewReader(conn)
		for {
			message, err := readWebSocketFrame(buffered)
			if err != nil {
				return
			}
			if len(message) > 0 {
				_ = session.write(string(message))
			}
		}
	}()

	events := session.subscribe()
	defer session.unsubscribe(events)
	for {
		select {
		case <-done:
			return
		case <-req.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				_ = writeWebSocketClose(conn)
				return
			}
			switch event.Type {
			case "output":
				if err := writeWebSocketBinary(conn, []byte(event.Data)); err != nil {
					return
				}
			case "exit":
				_ = writeWebSocketBinary(conn, []byte(fmt.Sprintf("\r\n[process exited %d]\r\n", event.Code)))
				_ = writeWebSocketClose(conn)
				return
			case "error":
				_ = writeWebSocketBinary(conn, []byte("\r\n[terminal error: "+event.Data+"]\r\n"))
			}
		}
	}
}

func acceptWebSocket(rw http.ResponseWriter, req *http.Request) (net.Conn, error) {
	if req.Method != http.MethodGet {
		return nil, errors.New("websocket requires GET")
	}
	if !headerContainsToken(req.Header, "Connection", "upgrade") || !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("websocket upgrade headers are required")
	}
	key := strings.TrimSpace(req.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("Sec-WebSocket-Key is required")
	}
	hijacker, ok := rw.(http.Hijacker)
	if !ok {
		return nil, errors.New("websocket hijacking is not supported")
	}
	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	accept := websocketAccept(key)
	_, err = fmt.Fprintf(bufrw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := bufrw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func readWebSocketFrame(reader io.Reader) ([]byte, error) {
	buffered, ok := reader.(*bufio.Reader)
	if !ok {
		buffered = bufio.NewReader(reader)
	}
	header := make([]byte, 2)
	if _, err := io.ReadFull(buffered, header); err != nil {
		return nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var raw [2]byte
		if _, err := io.ReadFull(buffered, raw[:]); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(raw[:]))
	case 127:
		var raw [8]byte
		if _, err := io.ReadFull(buffered, raw[:]); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(raw[:])
	}
	if length > 1<<20 {
		return nil, errors.New("websocket frame exceeds terminal input limit")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(buffered, mask[:]); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(buffered, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	switch opcode {
	case 0x1, 0x2:
		return payload, nil
	case 0x8:
		return nil, io.EOF
	case 0x9, 0xa:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported websocket opcode %d", opcode)
	}
}

func writeWebSocketBinary(writer io.Writer, payload []byte) error {
	return writeWebSocketFrame(writer, 0x2, payload)
}

func writeWebSocketClose(writer io.Writer) error {
	return writeWebSocketFrame(writer, 0x8, nil)
}

func writeWebSocketFrame(writer io.Writer, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 0xffff:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], uint64(length))
		header = append(header, raw[:]...)
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := writer.Write(payload)
	return err
}
