package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
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
	os.MkdirAll(c.sessionDir, 0700)

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

	c.client = whatsmeow.NewClient(deviceStore, nil)
	c.client.AddEventHandler(c.eventHandler)

	if c.client.Store.ID == nil {
		// Not logged in — print QR code
		qrChan, _ := c.client.GetQRChannel(ctx)
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
	<-ctx.Done()
	c.client.Disconnect()
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
		c.inbound <- channel.InboundMessage{
			Channel:  "whatsapp",
			ChatID:   v.Info.Chat.String(),
			SenderID: senderID,
			Text:     text,
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
	_, err = c.client.SendMessage(context.Background(), jid, &waE2E.Message{
		Conversation: proto.String(msg.Text),
	})
	return err
}

func printQR(code string) {
	fmt.Printf("\nWhatsApp QR Code:\n%s\n\nScan with WhatsApp -> Linked Devices -> Link a Device\n\n", code)
}
