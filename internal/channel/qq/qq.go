package qq

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	botgosdk "github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"

	"github.com/dfbb/im2code/internal/channel"
)

const (
	maxSeenIDs  = 1000
	sendTimeout = 10 * time.Second
	maxMsgLen   = 4000
)

// Channel is a QQ Bot adapter using the botgo WebSocket SDK (C2C private messages).
// Credentials (AppID + Secret) come from q.qq.com developer portal.
type Channel struct {
	appID     string
	secret    string
	allowFrom map[string]bool
	inbound   chan<- channel.InboundMessage

	mu      sync.RWMutex
	api     openapi.OpenAPI // set in Start, read in Send
	seenIDs map[string]struct{}
	seenSeq []string // insertion-ordered for eviction
}

func New(appID, secret string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool, len(allowFrom))
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{
		appID:     appID,
		secret:    secret,
		allowFrom: allow,
		inbound:   inbound,
		seenIDs:   make(map[string]struct{}),
	}
}

func (c *Channel) Name() string { return "qq" }

func (c *Channel) Start(ctx context.Context) error {
	ts := token.NewQQBotTokenSource(&token.QQBotCredentials{
		AppID:     c.appID,
		AppSecret: c.secret,
	})
	if err := token.StartRefreshAccessToken(ctx, ts); err != nil {
		return fmt.Errorf("qq: start token refresh: %w", err)
	}

	api := botgosdk.NewOpenAPI(c.appID, ts)
	c.mu.Lock()
	c.api = api
	c.mu.Unlock()

	intents := event.RegisterHandlers(event.C2CMessageEventHandler(c.onC2CMessage))

	ap, err := api.WS(ctx, nil, "")
	if err != nil {
		return fmt.Errorf("qq: get WS gateway: %w", err)
	}

	slog.Info("qq: starting WebSocket session", "shards", ap.Shards)
	go func() {
		if err := botgosdk.NewSessionManager().Start(ap, ts, &intents); err != nil {
			slog.Error("qq: session manager stopped", "err", err)
		}
	}()

	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error { return nil }

func (c *Channel) Send(msg channel.OutboundMessage) error {
	c.mu.RLock()
	api := c.api
	c.mu.RUnlock()
	if api == nil {
		return fmt.Errorf("qq: not started")
	}
	for _, chunk := range splitMessage(msg.Text, maxMsgLen) {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		_, err := api.PostC2CMessage(ctx, msg.ChatID, &dto.MessageToCreate{
			Content: chunk,
			MsgType: dto.TextMsg,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("qq: send: %w", err)
		}
	}
	return nil
}

func (c *Channel) onC2CMessage(_ *dto.WSPayload, data *dto.WSC2CMessageData) error {
	msg := (*dto.Message)(data)

	var userID string
	if msg.Author != nil {
		userID = msg.Author.ID
	}
	if userID == "" {
		return nil
	}

	if !c.addSeen(msg.ID) {
		return nil // duplicate
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return nil
	}

	if len(c.allowFrom) > 0 && !c.allowFrom[userID] {
		slog.Warn("qq: ignoring message from unauthorized user", "userID", userID)
		return nil
	}

	select {
	case c.inbound <- channel.InboundMessage{Channel: "qq", ChatID: userID, Text: content}:
	default:
		slog.Warn("qq: inbound full, dropping message", "userID", userID)
	}
	return nil
}

// addSeen records msgID and returns true if it was new (not a duplicate).
func (c *Channel) addSeen(msgID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seenIDs[msgID]; ok {
		return false
	}
	c.seenIDs[msgID] = struct{}{}
	c.seenSeq = append(c.seenSeq, msgID)
	if len(c.seenSeq) > maxSeenIDs {
		delete(c.seenIDs, c.seenSeq[0])
		c.seenSeq = c.seenSeq[1:]
	}
	return true
}

// CheckToken verifies the app credentials by fetching an access token.
func CheckToken(appID, secret string) (string, error) {
	ts := token.NewQQBotTokenSource(&token.QQBotCredentials{
		AppID:     appID,
		AppSecret: secret,
	})
	if _, err := ts.Token(); err != nil {
		return "", fmt.Errorf("qq: %w", err)
	}
	return "app_id=" + appID, nil
}

func splitMessage(text string, max int) []string {
	if len(text) <= max {
		return []string{text}
	}
	var chunks []string
	for len(text) > max {
		chunks = append(chunks, text[:max])
		text = text[max:]
	}
	if len(text) > 0 {
		chunks = append(chunks, text)
	}
	return chunks
}
