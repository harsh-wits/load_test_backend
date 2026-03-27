package callbacks

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (c *Controller) onSearch_handler(ctx *fiber.Ctx) error {
	receivedAt := time.Now().UTC()
	if err := c.verifyInbound("on_search", ctx); err != nil {
		return c.writeNack(ctx, err.Error())
	}
	if txnID := extractTxnID(ctx.Body()); txnID != "" && c.sessions != nil {
		_ = c.sessions.SetDiscoveryPayload(context.Background(), txnID, ctx.Body())
	}
	c.recordCallback("on_search", ctx, receivedAt)
	return c.writeAck(ctx)
}
