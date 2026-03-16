package apierror

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

func writeErrorResponse(c *fiber.Ctx, statusCode int, code, message string, details any) error {
	errMap := fiber.Map{"code": code, "message": message}
	if details != nil {
		errMap["details"] = details
	}
	return c.Status(statusCode).JSON(fiber.Map{
		"success":   false,
		"error":     errMap,
		"timestamp": time.Now().UTC(),
	})
}

func ErrorHandler() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		if c.Response().Header.ContentLength() > 0 && c.Response().StatusCode() >= 400 {
			return nil
		}

		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return writeErrorResponse(c, fiberErr.Code, fmt.Sprintf("HTTP_%d", fiberErr.Code), fiberErr.Message, nil)
		}

		var customErr *CustomError
		if errors.As(err, &customErr) {
			return writeErrorResponse(c, customErr.HTTPCode, customErr.Code, customErr.Message, customErr.Details)
		}

		log.Printf("[error_handler] unhandled error: %v", err)
		return writeErrorResponse(c, 500, "HTTP_500", "Internal Server Error", nil)
	}
}
