package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"github.com/mdp/qrterminal/v3"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"

	"github.com/dfbb/im2code/internal/channel"
)

type Channel struct {
	sessionDir  string
	allowFrom   map[string]bool
	onFirstUser func(string)
	inbound     chan<- channel.InboundMessage
	client      *whatsmeow.Client

	mu       sync.Mutex
	lockedID string
}

func New(sessionDir string, allowFrom []string, onFirstUser func(string), inbound chan<- channel.InboundMessage) *Channel {
	if sessionDir == "" {
		home, _ := os.UserHomeDir()
		sessionDir = home + "/.im2code/whatsapp"
	}
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	if len(allow) > 0 {
		onFirstUser = nil // static list set; auto-lock not needed
	}
	return &Channel{sessionDir: sessionDir, allowFrom: allow, onFirstUser: onFirstUser, inbound: inbound}
}

func (c *Channel) Name() string { return "whatsapp" }

func (c *Channel) Start(ctx context.Context) error {
	// Fix #2: handle MkdirAll error instead of silently ignoring it.
	if err := os.MkdirAll(c.sessionDir, 0700); err != nil {
		return fmt.Errorf("whatsapp: create session dir: %w", err)
	}

	// WAL mode allows concurrent reads alongside writes (prevents SQLITE_BUSY).
	// busy_timeout retries for up to 5 s instead of immediately returning BUSY.
	dsn := "file:" + c.sessionDir + "/session.db" +
		"?_pragma=foreign_keys(on)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)"
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

		if len(c.allowFrom) > 0 {
			// Static allow list.
			if !c.allowFrom[senderID] {
				return
			}
		} else {
			// Auto-lock: accept the first sender, reject everyone else.
			c.mu.Lock()
			if c.lockedID == "" {
				c.lockedID = senderID
				slog.Info("whatsapp: locked to first user", "senderID", senderID)
				if c.onFirstUser != nil {
					fn := c.onFirstUser
					go fn(senderID)
				}
			} else if c.lockedID != senderID {
				c.mu.Unlock()
				slog.Warn("whatsapp: ignoring message from non-locked user", "senderID", senderID, "lockedID", c.lockedID)
				return
			}
			c.mu.Unlock()
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
	for _, chunk := range splitMessage(msg.Text, 4000) {
		if err := c.sendChunk(jid, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) sendChunk(jid types.JID, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(text),
	})
	return err
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
		if current.Len() > 0 && current.Len()+len(line)+1 > maxLen {
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

func printQR(code string) {
	qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stderr)
}
