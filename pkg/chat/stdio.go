package chat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/andrewneudegg/lab/pkg/id"
)

type Stdio struct {
	In  io.Reader
	Out io.Writer
}

func (s Stdio) Name() string { return "stdio" }

func (s Stdio) Receive(ctx context.Context) (<-chan ChatMessage, error) {
	ch := make(chan ChatMessage)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(s.In)
		for scanner.Scan() {
			msg := ChatMessage{ID: id.New("msg"), Time: time.Now().UTC(), From: "terminal", Content: scanner.Text()}
			select {
			case <-ctx.Done():
				return
			case ch <- msg:
			}
		}
	}()
	return ch, nil
}

func (s Stdio) Send(ctx context.Context, msg OutboundMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		_, err := fmt.Fprintln(s.Out, msg.Content)
		return err
	}
}
