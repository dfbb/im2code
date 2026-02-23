package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/dfbb/im2code/internal/channel"
)

// Channel is the Telegram IM adapter. Uses HTTP long polling.
type Channel struct {
	token       string
	allowFrom   map[string]bool
	bot         *tgbotapi.BotAPI
	inbound     chan<- channel.InboundMessage
	onFirstUser func(string) // called once with the first sender ID when allowFrom is empty

	mu       sync.Mutex
	lockedID string // set after the first message when allowFrom is empty
}

// New creates a Telegram channel adapter.
// When allowFrom is empty, the first sender to message the bot is automatically
// locked in: their ID is stored in lockedID and onFirstUser is called so the
// caller can persist it (e.g. write it to the config file).
func New(token string, allowFrom []string, onFirstUser func(string), inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	// If an explicit list is already set, auto-lock is not needed.
	if len(allow) > 0 {
		onFirstUser = nil
	}
	return &Channel{token: token, allowFrom: allow, onFirstUser: onFirstUser, inbound: inbound}
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
		return // skip non-text messages (photos, stickers, voice, etc.)
	}
	senderID := fmt.Sprintf("%d", msg.From.ID)

	if len(c.allowFrom) > 0 {
		// Static allow list.
		if !c.allowFrom[senderID] && !c.allowFrom[msg.From.UserName] {
			return
		}
	} else {
		// Auto-lock: accept the first sender, reject everyone else.
		c.mu.Lock()
		if c.lockedID == "" {
			c.lockedID = senderID
			slog.Info("telegram: locked to first user", "senderID", senderID)
			if c.onFirstUser != nil {
				fn := c.onFirstUser
				go fn(senderID) // run in goroutine so it doesn't block the update loop
			}
		} else if c.lockedID != senderID {
			c.mu.Unlock()
			slog.Warn("telegram: ignoring message from non-locked user", "senderID", senderID, "lockedID", c.lockedID)
			return
		}
		c.mu.Unlock()
	}

	inMsg := channel.InboundMessage{
		Channel:  "telegram",
		ChatID:   fmt.Sprintf("%d", msg.Chat.ID),
		SenderID: senderID,
		Text:     msg.Text,
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
