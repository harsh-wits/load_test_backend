package callbacks

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func (c *Controller) onSelect(ctx *fiber.Ctx) error {
	receivedAt := time.Now().UTC()
	if err := c.verifyInbound("on_select", ctx); err != nil {
		c.recordCallbackFailure("on_select", ctx, receivedAt)
		return c.writeNack(ctx, err.Error())
	}
	if err := c.validatePayload("on_select", ctx); err != nil {
		c.recordCallbackFailure("on_select", ctx, receivedAt)
		return c.writeNack(ctx, err.Error())
	}
	c.recordCallback("on_select", ctx, receivedAt)
	return c.writeAck(ctx)
}
