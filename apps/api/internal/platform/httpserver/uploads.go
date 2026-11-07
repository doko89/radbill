package httpserver

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	authapp "boilerplate/api/internal/auth/application"
)

// serveUploads proxies /uploads/<type>/<uuid>.webp from the configured
// storage (local disk or S3). The strict allowlist (never a raw path join)
// makes traversal impossible by construction.
func serveUploads(store authapp.AvatarStorage) fiber.Handler {
	return func(c fiber.Ctx) error {
		assetType, file, ok := strings.Cut(c.Params("*"), "/")
		if !ok || !authapp.ValidAvatarPath(assetType, file) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
		}
		data, err := store.Load(c.Context(), assetType, file)
		if err != nil {
			if errors.Is(err, authapp.ErrAvatarNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
			}
			return err
		}
		setStaticSecurityHeaders(c)
		c.Set("Content-Type", "image/webp")
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.Send(data)
	}
}
