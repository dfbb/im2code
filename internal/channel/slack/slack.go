package slack

import (
	"context"
	"log/slog"
	"strings"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/dfbb/im2code/internal/channel"
)

// Channel is the Slack IM adapter. Uses Socket Mode (no public URL required).
type Channel struct {
	botToken  string
	appToken  string
	allowFrom map[string]bool
	inbound   chan<- channel.InboundMessage
	client    *goslack.Client
}

func New(botToken, appToken string, allowFrom []string, inbound chan<- channel.InboundMessage) *Channel {
	allow := make(map[string]bool)
	for _, id := range allowFrom {
		allow[id] = true
	}
	return &Channel{botToken: botToken, appToken: appToken, allowFrom: allow, inbound: inbound}
}

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(ctx context.Context) error {
	api := goslack.New(c.botToken, goslack.OptionAppLevelToken(c.appToken))
	c.client = api
	sm := socketmode.New(api)

	go func() {
		for evt := range sm.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				sm.Ack(*evt.Request)
				eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				if eventsAPI.Type == slackevents.CallbackEvent {
					c.handleInner(eventsAPI.InnerEvent)
				}
			}
		}
	}()

	authTest, err := api.AuthTest()
	if err != nil {
		return err
	}
	slog.Info("slack connected", "bot", authTest.User, "team", authTest.Team)
	return sm.RunContext(ctx)
}

func (c *Channel) handleInner(event slackevents.EventsAPIInnerEvent) {
	switch ev := event.Data.(type) {
	case *slackevents.MessageEvent:
		if ev.BotID != "" || ev.SubType != "" {
			return
		}
		if len(c.allowFrom) > 0 && !c.allowFrom[ev.User] {
			return
		}
		c.inbound <- channel.InboundMessage{
			Channel:  "slack",
			ChatID:   ev.Channel,
			SenderID: ev.User,
			Text:     ev.Text,
		}
	}
}

func (c *Channel) Stop() error { return nil }

func (c *Channel) Send(msg channel.OutboundMessage) error {
	if c.client == nil {
		return nil
	}
	for _, chunk := range splitMessage(msg.Text, 3000) {
		if _, _, err := c.client.PostMessage(msg.ChatID,
			goslack.MsgOptionText(chunk, false),
		); err != nil {
			return err
		}
	}
	return nil
}

// CheckToken verifies the bot token via Slack's auth.test API.
func CheckToken(botToken string) (string, error) {
	api := goslack.New(botToken)
	auth, err := api.AuthTest()
	if err != nil {
		return "", err
	}
	return auth.User + " (" + auth.Team + ")", nil
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
