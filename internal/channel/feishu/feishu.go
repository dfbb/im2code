package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/dfbb/im2code/internal/channel"
)

// Channel is the Feishu (Lark) IM adapter. Uses WebSocket long-connection.
type Channel struct {
	appID     string
	appSecret string
	allowFrom map[string]bool
	inbound   chan<- channel.InboundMessage
	apiClient *lark.Client
}

func New(appID, appSecret string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{
		appID:     appID,
		appSecret: appSecret,
		allowFrom: allow,
		inbound:   inbound,
		apiClient: lark.NewClient(appID, appSecret),
	}
}

func (c *Channel) Name() string { return "feishu" }

func (c *Channel) Start(ctx context.Context) error {
	// Build event dispatcher (no verification token / encrypt key needed for WS mode).
	d := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(c.onMessage)

	wsClient := larkws.NewClient(c.appID, c.appSecret,
		larkws.WithEventHandler(d),
		larkws.WithLogLevel(larkcore.LogLevelWarn),
	)

	slog.Info("feishu: starting WebSocket client")
	// wsClient.Start blocks until ctx is cancelled (auto-reconnect handled inside).
	return wsClient.Start(ctx)
}

func (c *Channel) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil {
		return nil
	}

	ev := event.Event

	// Extract sender ID (open_id).
	var senderID string
	if ev.Sender != nil && ev.Sender.SenderId != nil && ev.Sender.SenderId.OpenId != nil {
		senderID = *ev.Sender.SenderId.OpenId
	}

	// Apply allowFrom filter.
	if len(c.allowFrom) > 0 && !c.allowFrom[senderID] {
		return nil
	}

	// Extract chat ID.
	var chatID string
	if ev.Message != nil && ev.Message.ChatId != nil {
		chatID = *ev.Message.ChatId
	}

	// Extract text content. Feishu content is JSON: {"text":"..."}.
	var text string
	if ev.Message != nil && ev.Message.Content != nil {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(*ev.Message.Content), &payload); err == nil {
			text = payload.Text
		} else {
			text = *ev.Message.Content
		}
	}

	msg := channel.InboundMessage{
		Channel:  "feishu",
		ChatID:   chatID,
		SenderID: senderID,
		Text:     text,
	}

	select {
	case c.inbound <- msg:
	default:
		slog.Warn("feishu: inbound channel full, dropping message", "chatID", chatID)
	}
	return nil
}

func (c *Channel) Stop() error {
	// WebSocket client stops when its context is cancelled.
	return nil
}

func (c *Channel) sendChunk(receiveID, content string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(content).
			Build()).
		Build()
	resp, err := c.apiClient.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("feishu send error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	if c.apiClient == nil {
		return fmt.Errorf("feishu: not started")
	}
	for _, chunk := range splitMessage(msg.Text, 4000) {
		// JSON-encode the text to produce a valid Feishu content string.
		contentBytes, err := json.Marshal(map[string]string{"text": chunk})
		if err != nil {
			return fmt.Errorf("feishu: marshal content: %w", err)
		}
		if err := c.sendChunk(msg.ChatID, string(contentBytes)); err != nil {
			return fmt.Errorf("feishu: send: %w", err)
		}
	}
	return nil
}

// Connect establishes a brief WebSocket connection to the Feishu platform.
// Feishu requires at least one successful WS connection before the long-connection
// mode option becomes available in the developer console.
func Connect(appID, appSecret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	d := dispatcher.NewEventDispatcher("", "")
	wsClient := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(d),
		larkws.WithLogLevel(larkcore.LogLevelError),
	)

	err := wsClient.Start(ctx)
	// Context timeout is the expected exit path — connection was successfully established.
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// CheckToken verifies the app credentials by fetching a tenant access token.
func CheckToken(appID, appSecret string) (string, error) {
	body := fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, appID, appSecret)
	resp, err := http.Post(
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		"application/json; charset=utf-8",
		strings.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("feishu: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("feishu: decode response: %w", err)
	}
	if r.Code != 0 {
		return "", fmt.Errorf("feishu: %s (code %d)", r.Msg, r.Code)
	}
	return "app_id=" + appID, nil
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
