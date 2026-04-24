package chat

import (
	"context"
	"time"
)

type ChatMessage struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	From    string    `json:"from"`
	Content string    `json:"content"`
}

type OutboundMessage struct {
	To      string `json:"to,omitempty"`
	Content string `json:"content"`
}

type Adapter interface {
	Name() string
	Receive(ctx context.Context) (<-chan ChatMessage, error)
	Send(ctx context.Context, msg OutboundMessage) error
}
