package callbacks

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func (c *Controller) onConfirm(ctx *fiber.Ctx) error {
	receivedAt := time.Now().UTC()
	if err := c.verifyInbound("on_confirm", ctx); err != nil {
		c.recordCallbackFailure("on_confirm", ctx, receivedAt)
		return c.writeNack(ctx, err.Error())
	}
	if err := c.validatePayload("on_confirm", ctx); err != nil {
		c.recordCallbackFailure("on_confirm", ctx, receivedAt)
		return c.writeNack(ctx, err.Error())
	}
	c.recordCallback("on_confirm", ctx, receivedAt)
	return c.writeAck(ctx)
}
