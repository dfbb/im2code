package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/dfbb/im2code/internal/channel"
)

// Channel is the Telegram IM adapter. Uses HTTP long polling.
type Channel struct {
	token     string
	allowFrom map[string]bool
	bot       *tgbotapi.BotAPI
	inbound   chan<- channel.InboundMessage
}

func New(token string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{token: token, allowFrom: allow, inbound: inbound}
}

func (c *Channel) Name() string { return "telegram" }

func (c *Channel) Start(ctx context.Context) error {
	bot, err := tgbotapi.NewBotAPI(c.token)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	c.bot = bot
	slog.Info("telegram connected", "bot", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message == nil {
				continue
			}
			c.handleUpdate(update)
		}
	}
}

func (c *Channel) handleUpdate(update tgbotapi.Update) {
	msg := update.Message
	if msg.Text == "" {
		return // skip non-text messages
	}
	senderID := fmt.Sprintf("%d", msg.From.ID)

	preAuthorized := false
	if len(c.allowFrom) > 0 {
		if !c.allowFrom[senderID] && !c.allowFrom[msg.From.UserName] {
			return
		}
		preAuthorized = true
	}

	inMsg := channel.InboundMessage{
		Channel:       "telegram",
		ChatID:        fmt.Sprintf("%d", msg.Chat.ID),
		SenderID:      senderID,
		Text:          msg.Text,
		PreAuthorized: preAuthorized,
	}
	select {
	case c.inbound <- inMsg:
	default:
		slog.Warn("telegram: inbound queue full, dropping message", "sender", senderID)
	}
}

func (c *Channel) Stop() error {
	if c.bot != nil {
		c.bot.StopReceivingUpdates()
	}
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	if c.bot == nil {
		return fmt.Errorf("telegram: not connected")
	}
	chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", msg.ChatID, err)
	}
	for _, chunk := range splitMessage(msg.Text, 4000) {
		m := tgbotapi.NewMessage(chatID, chunk)
		m.ParseMode = "Markdown"
		if _, err := c.bot.Send(m); err != nil {
			// Retry without markdown on parse error
			m.ParseMode = ""
			if _, err2 := c.bot.Send(m); err2 != nil {
				return err2
			}
		}
	}
	return nil
}

// CheckToken verifies the bot token and returns the bot username.
func CheckToken(token string) (string, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return "", err
	}
	return "@" + bot.Self.UserName, nil
}

func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	lines := strings.Split(text, "\n")
	var cur strings.Builder
	for _, line := range lines {
		if cur.Len() > 0 && cur.Len()+len(line)+1 > maxLen {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		cur.WriteString(line + "\n")
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}
