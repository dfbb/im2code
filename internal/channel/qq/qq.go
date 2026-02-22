package qq

import (
	"context"
	"log/slog"

	"github.com/dfbb/im2code/internal/channel"
)

// Channel is a skeleton QQ Bot adapter.
// Full implementation requires QQ Bot Gateway app registration and is similar
// in structure to the Discord adapter. It is left as a placeholder.
type Channel struct {
	inbound chan<- channel.InboundMessage
}

func New(inbound chan<- channel.InboundMessage) *Channel {
	return &Channel{inbound: inbound}
}

func (c *Channel) Name() string { return "qq" }

func (c *Channel) Start(ctx context.Context) error {
	slog.Warn("qq: adapter not fully implemented; blocking until context done")
	<-ctx.Done()
	return nil
}

func (c *Channel) Stop() error {
	return nil
}

func (c *Channel) Send(msg channel.OutboundMessage) error {
	return nil
}
