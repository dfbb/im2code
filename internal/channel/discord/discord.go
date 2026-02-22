package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/dfbb/im2code/internal/channel"
)

const (
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	apiBase    = "https://discord.com/api/v10"
)

type payload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int            `json:"s"`
	T  *string         `json:"t"`
}

// Channel is the Discord IM adapter. Uses the Discord Gateway WebSocket.
type Channel struct {
	token     string
	allowFrom map[string]bool
	inbound   chan<- channel.InboundMessage
	mu        sync.Mutex
	ws        *websocket.Conn
	seq       int
	botID     string
}

func New(token string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{token: token, allowFrom: allow, inbound: inbound}
}

func (c *Channel) Name() string { return "discord" }

func (c *Channel) Start(ctx context.Context) error {
	for {
		if err := c.connect(ctx); err != nil {
			slog.Error("discord connection error", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			slog.Info("discord reconnecting...")
		}
	}
}

func (c *Channel) connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.ws = conn
	c.mu.Unlock()
	defer func() {
		conn.Close()
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var p payload
		if err := json.Unmarshal(msg, &p); err != nil {
			continue
		}
		if p.S != nil {
			c.seq = *p.S
		}

		switch p.Op {
		case 10: // HELLO
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			json.Unmarshal(p.D, &hello)
			go c.heartbeat(ctx, conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond)
			if err := c.identify(conn); err != nil {
				return err
			}
		case 0: // DISPATCH
			if p.T == nil {
				continue
			}
			switch *p.T {
			case "READY":
				var ready struct {
					User struct {
						ID       string `json:"id"`
						Username string `json:"username"`
					} `json:"user"`
				}
				json.Unmarshal(p.D, &ready)
				c.botID = ready.User.ID
				slog.Info("discord connected", "bot", ready.User.Username)
			case "MESSAGE_CREATE":
				c.handleMessage(p.D)
			}
		}
	}
}

func (c *Channel) identify(conn *websocket.Conn) error {
	data, _ := json.Marshal(map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   c.token,
			"intents": 33280, // GUILD_MESSAGES + MESSAGE_CONTENT + DIRECT_MESSAGES
			"properties": map[string]string{
				"os": "linux", "browser": "im2code", "device": "im2code",
			},
		},
	})
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Channel) heartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			data, _ := json.Marshal(map[string]any{"op": 1, "d": c.seq})
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

func (c *Channel) handleMessage(d json.RawMessage) {
	var msg struct {
		Content string `json:"content"`
		Author  struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		ChannelID string `json:"channel_id"`
	}
	json.Unmarshal(d, &msg)

	if msg.Author.Bot || msg.Author.ID == c.botID {
		return
	}
	if len(c.allowFrom) > 0 && !c.allowFrom[msg.Author.ID] {
		return
	}

	c.inbound <- channel.InboundMessage{
		Channel:  "discord",
		ChatID:   msg.ChannelID,
		SenderID: msg.Author.ID,
		Text:     msg.Content,
	}
}

func (c *Channel) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		c.ws.Close()
	}
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	url := fmt.Sprintf("%s/channels/%s/messages", apiBase, msg.ChatID)
	for _, chunk := range splitMessage(msg.Text, 2000) {
		body, _ := json.Marshal(map[string]string{"content": chunk})
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bot "+c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode == 429 {
			// Simple rate limit backoff
			time.Sleep(1 * time.Second)
		}
	}
	return nil
}

// CheckToken verifies the bot token by calling the Discord API.
func CheckToken(token string) (string, error) {
	req, _ := http.NewRequest("GET", apiBase+"/users/@me", nil)
	req.Header.Set("Authorization", "Bot "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var u struct {
		Username string `json:"username"`
	}
	json.NewDecoder(resp.Body).Decode(&u)
	return "@" + u.Username, nil
}

func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	lines := strings.Split(text, "\n")
	var cur strings.Builder
	for _, line := range lines {
		if cur.Len()+len(line)+1 > maxLen {
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
