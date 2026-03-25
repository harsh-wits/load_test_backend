package callbacks

import "github.com/gofiber/fiber/v2"

func (c *Controller) onInit(ctx *fiber.Ctx) error {
	if err := c.verifyInbound("on_init", ctx); err != nil {
		c.recordCallbackFailure("on_init", ctx)
		return c.writeNack(ctx, err.Error())
	}
	if err := c.validatePayload("on_init", ctx); err != nil {
		c.recordCallbackFailure("on_init", ctx)
		return c.writeNack(ctx, err.Error())
	}
	c.recordCallback("on_init", ctx)
	return c.writeAck(ctx)
}
