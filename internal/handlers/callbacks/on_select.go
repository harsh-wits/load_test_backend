package callbacks

import "github.com/gofiber/fiber/v2"

func (c *Controller) onSelect(ctx *fiber.Ctx) error {
	if err := c.verifyInbound("on_select", ctx); err != nil {
		c.recordCallbackFailure("on_select", ctx)
		return c.writeNack(ctx, err.Error())
	}
	if err := c.validatePayload("on_select", ctx); err != nil {
		c.recordCallbackFailure("on_select", ctx)
		return c.writeNack(ctx, err.Error())
	}
	c.recordCallback("on_select", ctx)
	return c.writeAck(ctx)
}
