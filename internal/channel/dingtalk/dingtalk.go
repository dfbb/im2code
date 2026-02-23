package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"

	"github.com/dfbb/im2code/internal/channel"
)

// Channel is the DingTalk IM adapter. Uses the DingTalk Stream SDK (WebSocket).
type Channel struct {
	clientID     string
	clientSecret string
	allowFrom    map[string]bool
	inbound      chan<- channel.InboundMessage
	cli          *client.StreamClient
}

func New(clientID, clientSecret string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{
		clientID:     clientID,
		clientSecret: clientSecret,
		allowFrom:    allow,
		inbound:      inbound,
	}
}

func (c *Channel) Name() string { return "dingtalk" }

func (c *Channel) Start(ctx context.Context) error {
	c.cli = client.NewStreamClient(
		client.WithAppCredential(client.NewAppCredentialConfig(c.clientID, c.clientSecret)),
	)

	c.cli.RegisterChatBotCallbackRouter(c.onMessage)

	slog.Info("dingtalk: starting stream client")
	if err := c.cli.Start(ctx); err != nil {
		return err
	}

	// c.cli.Start does not block; wait until context is cancelled.
	<-ctx.Done()
	return nil
}

func (c *Channel) onMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	if data == nil {
		return nil, nil
	}

	senderID := data.SenderStaffId
	chatID := data.ConversationId
	text := data.Text.Content
	if text == "" {
		return nil, nil // non-text message, skip
	}

	// Apply allowFrom filter.
	if len(c.allowFrom) > 0 && !c.allowFrom[senderID] {
		return nil, nil
	}

	msg := channel.InboundMessage{
		Channel:  "dingtalk",
		ChatID:   chatID,
		SenderID: senderID,
		Text:     text,
	}

	select {
	case c.inbound <- msg:
	default:
		slog.Warn("dingtalk: inbound channel full, dropping message", "chatID", chatID)
	}

	return nil, nil
}

func (c *Channel) Stop() error {
	if c.cli != nil {
		c.cli.Close()
	}
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	slog.Warn("dingtalk: Send not implemented; requires per-message session webhook", "chatID", msg.ChatID)
	return fmt.Errorf("dingtalk: Send not implemented")
}

// CheckToken verifies the client credentials by fetching an OAuth2 access token.
func CheckToken(clientID, clientSecret string) (string, error) {
	body := fmt.Sprintf(`{"clientId":%q,"clientSecret":%q,"grantType":"client_credentials"}`, clientID, clientSecret)
	resp, err := http.Post(
		"https://api.dingtalk.com/v1.0/oauth2/accessToken",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("dingtalk: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		AccessToken string `json:"accessToken"`
		ErrCode     string `json:"code"`
		ErrMsg      string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if r.AccessToken == "" {
		if r.ErrMsg != "" {
			return "", fmt.Errorf("dingtalk: %s", r.ErrMsg)
		}
		return "", fmt.Errorf("dingtalk: no access token in response (status %d)", resp.StatusCode)
	}
	return "client_id=" + clientID, nil
}
