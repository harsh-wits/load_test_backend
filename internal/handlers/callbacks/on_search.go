package callbacks

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

func (c *Controller) onSearch_handler(ctx *fiber.Ctx) error {
	if err := c.verifyInbound("on_search", ctx); err != nil {
		return c.writeNack(ctx, err.Error())
	}
	if txnID := extractTxnID(ctx.Body()); txnID != "" && c.sessions != nil {
		_ = c.sessions.SetDiscoveryPayload(context.Background(), txnID, ctx.Body())
	}
	c.recordCallback("on_search", ctx)
	return c.writeAck(ctx)
}
