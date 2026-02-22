package channel

import (
	"context"
	"log/slog"
)

// Channel is implemented by each IM platform adapter.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(msg OutboundMessage) error
}

type InboundMessage struct {
	Channel  string
	ChatID   string
	SenderID string
	Text     string
	Media    []string
}

type OutboundMessage struct {
	Channel string
	ChatID  string
	Text    string
	Media   []string
}

// Manager runs all channels and routes outbound messages.
type Manager struct {
	channels map[string]Channel
	inbound  chan<- InboundMessage
	outbound <-chan OutboundMessage
}

func NewManager(inbound chan<- InboundMessage, outbound <-chan OutboundMessage) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		inbound:  inbound,
		outbound: outbound,
	}
}

func (m *Manager) Register(ch Channel) {
	m.channels[ch.Name()] = ch
}

// Run starts all channels and dispatches outbound messages. Blocks until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	for _, ch := range m.channels {
		go func(c Channel) {
			if err := c.Start(ctx); err != nil {
				slog.Error("channel error", "channel", c.Name(), "err", err)
			}
		}(ch)
	}
	for {
		select {
		case <-ctx.Done():
			for _, ch := range m.channels {
				ch.Stop()
			}
			return
		case msg := <-m.outbound:
			ch, ok := m.channels[msg.Channel]
			if !ok {
				slog.Warn("unknown channel", "channel", msg.Channel)
				continue
			}
			if err := ch.Send(msg); err != nil {
				slog.Error("send error", "channel", msg.Channel, "err", err)
			}
		}
	}
}
