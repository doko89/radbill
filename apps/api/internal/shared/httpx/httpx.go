// Package httpx holds tiny JSON helpers shared by HTTP presentations.
package httpx

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/shared/apperr"
)

// Error writes {"error": msg} with the given status.
func Error(c fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

// FieldError writes a 400 for *apperr.FieldError.
// It reports handled=false when err is not a field error.
func FieldError(c fiber.Ctx, err error) (bool, error) {
	var fe *apperr.FieldError
	if !errors.As(err, &fe) {
		return false, nil
	}
	return true, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error":  "validation failed",
		"fields": fiber.Map{fe.Field: fe.Message},
	})
}
