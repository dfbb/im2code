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
	"github.com/mdp/qrterminal/v3"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"

	"github.com/dfbb/im2code/internal/channel"
)

type Channel struct {
	sessionDir string
	logLevel   string
	allowFrom  map[string]bool
	inbound    chan<- channel.InboundMessage
	client     *whatsmeow.Client
}

func New(sessionDir string, allowFrom []string, logLevel string, inbound chan<- channel.InboundMessage) *Channel {
	if sessionDir == "" {
		home, _ := os.UserHomeDir()
		sessionDir = home + "/.im2code/whatsapp"
	}
	if logLevel == "" {
		logLevel = "WARN"
	}
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{sessionDir: sessionDir, logLevel: strings.ToUpper(logLevel), allowFrom: allow, inbound: inbound}
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

	c.client = whatsmeow.NewClient(deviceStore, waLog.Stdout("whatsapp", c.logLevel, true))
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
		slog.Debug("whatsapp: raw event",
			"type", v.Info.Type,
			"from", v.Info.Sender,
			"chat", v.Info.Chat,
			"isFromMe", v.Info.IsFromMe,
			"myDevice", func() uint16 {
				if c.client.Store.ID != nil {
					return c.client.Store.ID.Device
				}
				return 0
			}(),
		)

		// Skip only messages sent by this device itself (echoes of our own replies).
		// Messages from other devices of the same account (e.g. the user's phone)
		// have IsFromMe=true but a different Device number — those must be processed.
		if v.Info.IsFromMe {
			if c.client.Store.ID != nil && v.Info.Sender.Device == c.client.Store.ID.Device {
				slog.Debug("whatsapp: skipping own echo", "device", v.Info.Sender.Device)
				return
			}
			slog.Debug("whatsapp: allowing IsFromMe from other device", "senderDevice", v.Info.Sender.Device)
		}

		// Skip non-text events (receipts, stickers, system messages).
		text := ""
		if v.Message.GetConversation() != "" {
			text = v.Message.GetConversation()
		} else if v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}
		slog.Debug("whatsapp: extracted text", "text", text, "len", len(text))
		if text == "" {
			slog.Debug("whatsapp: skipping (no text)")
			return
		}

		// Strip device suffix (number:3@s.whatsapp.net → number@s.whatsapp.net)
		// so the same person is recognised across all their devices.
		senderID := v.Info.Sender.ToNonAD().String()
		slog.Debug("whatsapp: senderID", "senderID", senderID, "allowFrom", c.allowFrom)

		preAuthorized := false
		if len(c.allowFrom) > 0 {
			if !c.allowFrom[senderID] {
				slog.Debug("whatsapp: skipping (not in allowFrom)")
				return
			}
			preAuthorized = true
		}

		msg := channel.InboundMessage{
			Channel:       "whatsapp",
			ChatID:        v.Info.Chat.String(),
			SenderID:      senderID,
			Text:          text,
			PreAuthorized: preAuthorized,
		}
		slog.Debug("whatsapp: sending to inbound", "chatID", msg.ChatID, "preAuthorized", preAuthorized)
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
