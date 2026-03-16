package callbacks

import "github.com/gofiber/fiber/v2"

func (c *Controller) onConfirm(ctx *fiber.Ctx) error {
	if err := c.verifyInbound("on_confirm", ctx); err != nil {
		return c.writeNack(ctx, err.Error())
	}
	if err := c.validatePayload("on_confirm", ctx); err != nil {
		return c.writeNack(ctx, err.Error())
	}
	c.recordCallback("on_confirm", ctx)
	return c.writeAck(ctx)
}
