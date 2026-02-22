package channel_test

import (
	"context"
	"testing"
	"time"

	"github.com/dfbb/im2code/internal/channel"
)

// mockChannel implements Channel for testing
type mockChannel struct {
	name string
	sent []channel.OutboundMessage
}

func (m *mockChannel) Name() string                      { return m.name }
func (m *mockChannel) Start(_ context.Context) error     { return nil }
func (m *mockChannel) Stop() error                       { return nil }
func (m *mockChannel) Send(msg channel.OutboundMessage) error {
	m.sent = append(m.sent, msg)
	return nil
}

func TestChannelInterface(t *testing.T) {
	var ch channel.Channel = &mockChannel{name: "test"}
	if ch.Name() != "test" {
		t.Errorf("Name() = %q, want %q", ch.Name(), "test")
	}
}

func TestManagerRouteOutbound(t *testing.T) {
	inbound := make(chan channel.InboundMessage, 1)
	outbound := make(chan channel.OutboundMessage, 1)
	mock := &mockChannel{name: "telegram"}

	mgr := channel.NewManager(inbound, outbound)
	mgr.Register(mock)

	msg := channel.OutboundMessage{
		Channel: "telegram",
		ChatID:  "123",
		Text:    "hello",
	}
	outbound <- msg

	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Run(ctx)

	// give dispatcher time to process
	time.Sleep(50 * time.Millisecond)
	cancel()

	if len(mock.sent) != 1 || mock.sent[0].Text != "hello" {
		t.Errorf("expected message to be dispatched to mock channel, got %v", mock.sent)
	}
}
