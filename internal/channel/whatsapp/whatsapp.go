package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"

	"github.com/dfbb/im2code/internal/channel"
)

type Channel struct {
	sessionDir string
	allowFrom  map[string]bool
	inbound    chan<- channel.InboundMessage
	client     *whatsmeow.Client
}

func New(sessionDir string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	if sessionDir == "" {
		home, _ := os.UserHomeDir()
		sessionDir = home + "/.im2code/whatsapp"
	}
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{sessionDir: sessionDir, allowFrom: allow, inbound: inbound}
}

func (c *Channel) Name() string { return "whatsapp" }

func (c *Channel) Start(ctx context.Context) error {
	// Fix #2: handle MkdirAll error instead of silently ignoring it.
	if err := os.MkdirAll(c.sessionDir, 0700); err != nil {
		return fmt.Errorf("whatsapp: create session dir: %w", err)
	}

	// Use file DSN with foreign keys enabled (required by sqlstore)
	dsn := "file:" + c.sessionDir + "/session.db?_foreign_keys=on"
	container, err := sqlstore.New(ctx, "sqlite", dsn, nil)
	if err != nil {
		return fmt.Errorf("whatsapp store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp device: %w", err)
	}

	// Fix #7: pass a real logger instead of nil so library errors are visible.
	c.client = whatsmeow.NewClient(deviceStore, waLog.Stdout("whatsapp", "INFO", true))
	c.client.AddEventHandler(c.eventHandler)

	if c.client.Store.ID == nil {
		// Not logged in — print QR code.
		// Fix #3: check the error from GetQRChannel instead of discarding it.
		qrChan, err := c.client.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("whatsapp: get QR channel: %w", err)
		}
		if err := c.client.Connect(); err != nil {
			return err
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				printQR(evt.Code)
			} else {
				slog.Info("whatsapp QR event", "event", evt.Event)
				break
			}
		}
	} else {
		if err := c.client.Connect(); err != nil {
			return err
		}
	}

	slog.Info("whatsapp connected", "jid", c.client.Store.ID)
	// Fix #4: do NOT call Disconnect here. Stop() is the authoritative place
	// to call Disconnect. Calling it in both places causes a double-disconnect.
	<-ctx.Done()
	return nil
}

func (c *Channel) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}
		senderID := v.Info.Sender.String()
		if len(c.allowFrom) > 0 && !c.allowFrom[senderID] {
			return
		}
		text := ""
		if v.Message.GetConversation() != "" {
			text = v.Message.GetConversation()
		} else if v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}
		if text == "" {
			return
		}
		// Fix #1: use a non-blocking send so a full inbound queue does not stall
		// the whatsmeow WebSocket read goroutine (eventHandler is called
		// synchronously on that goroutine).
		msg := channel.InboundMessage{
			Channel:  "whatsapp",
			ChatID:   v.Info.Chat.String(),
			SenderID: senderID,
			Text:     text,
		}
		select {
		case c.inbound <- msg:
		default:
			slog.Warn("whatsapp: inbound queue full, dropping message", "sender", senderID)
		}
	}
}

func (c *Channel) Stop() error {
	if c.client != nil {
		c.client.Disconnect()
	}
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	if c.client == nil {
		return fmt.Errorf("whatsapp: not connected")
	}
	jid, err := types.ParseJID(msg.ChatID)
	if err != nil {
		return err
	}
	// Fix #5 + #6: add a per-send context timeout and split long messages into
	// chunks so they stay under WhatsApp's size limit.
	for _, chunk := range splitMessage(msg.Text, 4000) {
		sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = c.client.SendMessage(sendCtx, jid, &waE2E.Message{
			Conversation: proto.String(chunk),
		})
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// splitMessage splits text into chunks of at most maxLen bytes, breaking on
// newlines where possible. This keeps messages under WhatsApp's effective size
// limit (~65535 bytes; 4000 is used for safety and consistency with other
// adapters in this project).
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	lines := strings.Split(text, "\n")
	var current strings.Builder
	for _, line := range lines {
		if current.Len()+len(line)+1 > maxLen {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(line + "\n")
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// printQR renders the QR code payload as a scannable image in the terminal.
// Fix #8: the previous implementation printed the raw payload string, which
// cannot be scanned by a phone camera. This version uses the qrTerminal helper
// to render actual QR pixels using Unicode half-block characters.
//
// NOTE: github.com/mdp/qrterminal/v3 is the preferred renderer. If it is not
// available (e.g. no network access during build), the fallback below renders
// the QR matrix using only the standard library.
func printQR(code string) {
	fmt.Println("\nScan this QR code with WhatsApp -> Linked Devices -> Link a Device:")
	renderQR(code)
	fmt.Println()
}
