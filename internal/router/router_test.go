package router_test

import (
	"os"
	"testing"

	"github.com/dfbb/im2code/internal/channel"
	"github.com/dfbb/im2code/internal/router"
	"github.com/dfbb/im2code/internal/state"
)

func newTestRouter(t *testing.T) (*router.Router, chan channel.OutboundMessage) {
	t.Helper()
	f, _ := os.CreateTemp("", "subs*.json")
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	subs, _ := state.NewSubscriptions(f.Name())
	outbound := make(chan channel.OutboundMessage, 10)
	r := router.New("#", subs, nil, outbound)
	return r, outbound
}

func TestRoute_NoSession(t *testing.T) {
	r, outbound := newTestRouter(t)

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", SenderID: "u1",
		Text: "hello",
	})

	msg := <-outbound
	if msg.ChatID != "123" {
		t.Errorf("expected reply to 123, got %q", msg.ChatID)
	}
	if msg.Text == "" {
		t.Error("expected non-empty reply when no session bound")
	}
}

func TestRoute_HashHelp(t *testing.T) {
	r, outbound := newTestRouter(t)

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", SenderID: "u1",
		Text: "#help",
	})

	msg := <-outbound
	if msg.ChatID != "123" {
		t.Errorf("expected reply to 123, got %q", msg.ChatID)
	}
	if msg.Text == "" {
		t.Error("expected non-empty help text")
	}
}

func TestRoute_AttachStatus(t *testing.T) {
	r, outbound := newTestRouter(t)

	// Attach to a session
	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "#attach mysession",
	})
	msg := <-outbound
	if msg.Text == "" {
		t.Error("expected confirmation of attach")
	}

	// Check status
	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "#status",
	})
	msg = <-outbound
	if msg.Text == "" {
		t.Error("expected status reply")
	}
}

func TestRoute_Detach(t *testing.T) {
	r, outbound := newTestRouter(t)

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "#attach mysession",
	})
	<-outbound // consume attach reply

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "#detach",
	})
	msg := <-outbound
	if msg.Text == "" {
		t.Error("expected detach confirmation")
	}
}

func TestRoute_CustomPrefix(t *testing.T) {
	f, _ := os.CreateTemp("", "subs*.json")
	f.Close()
	defer os.Remove(f.Name())

	subs, _ := state.NewSubscriptions(f.Name())
	outbound := make(chan channel.OutboundMessage, 10)
	r := router.New("!", subs, nil, outbound)

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "!help",
	})
	msg := <-outbound
	if msg.Text == "" {
		t.Error("expected help response with custom prefix")
	}
}

func TestRoute_UnknownCommand(t *testing.T) {
	r, outbound := newTestRouter(t)

	r.Handle(channel.InboundMessage{
		Channel: "telegram", ChatID: "123", Text: "#foobar",
	})
	msg := <-outbound
	if msg.Text == "" {
		t.Error("expected error reply for unknown command")
	}
}
