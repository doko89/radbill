// Package presentation adapts jwtauth use cases to HTTP and exposes the
// RBAC middleware for billing routes.
package presentation

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/shared/apperr"
)

// errorBody is the billing error envelope: {error:{code,message,field?}}.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeFieldError maps *apperr.FieldError to 400. handled=false otherwise.
func writeFieldError(c fiber.Ctx, err error) (bool, error) {
	var fe *apperr.FieldError
	if !errors.As(err, &fe) {
		return false, nil
	}
	return true, c.Status(fiber.StatusBadRequest).JSON(errorBody{
		Error: errorDetail{Code: "invalid", Message: fe.Message, Field: fe.Field},
	})
}

func writeItem(c fiber.Ctx, status int, v any) error {
	return c.Status(status).JSON(fiber.Map{"data": v})
}

// mapAuthError maps jwtauth domain errors to HTTP responses.
func mapAuthError(c fiber.Ctx, err error) error {
	if handled, herr := writeFieldError(c, err); handled {
		return herr
	}
	switch {
	case errors.Is(err, authdomain.ErrInvalidCredentials):
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid username or password")
	case errors.Is(err, authdomain.ErrAccountDisabled):
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "account disabled")
	case errors.Is(err, authdomain.ErrAccountNotFound),
		errors.Is(err, authdomain.ErrTokenInvalid):
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	case errors.Is(err, authdomain.ErrTooManyAttempts):
		return writeError(c, fiber.StatusTooManyRequests, "rate_limited", "too many login attempts, try again later")
	case errors.Is(err, authdomain.ErrUsernameTaken):
		return writeError(c, fiber.StatusConflict, "conflict", "username already taken")
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
}
