package presentation

import (
	"github.com/gofiber/fiber/v3"

	authapp "boilerplate/api/internal/jwtauth/application"
	authdomain "boilerplate/api/internal/jwtauth/domain"
)

// Billing Locals keys read by the G2 portal handlers (C2 kriskris, C3 portal).
// Cross-agent contract: the literals "billing_customer_id" and "billing_role"
// are dictated by the orchestrator; C2/C3 read them without importing this
// package, so never rename the values.
const (
	BillingCustomerIDKey = "billing_customer_id"
	BillingRoleKey       = "billing_role"
)

// RequireWithBillingContext enforces one RBAC permission exactly like Require
// and additionally stores the billing locals for portal handlers:
// BillingCustomerIDKey ("" for admins without a linked customer) and
// BillingRoleKey. Existing Require behavior is untouched (G2, ADITIF).
func RequireWithBillingContext(svc *authapp.Service, perm string) fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, err := verifyBearer(svc, c)
		if err != nil {
			return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
		}
		if !authdomain.HasPermission(claims.Role, perm) {
			return writeError(c, fiber.StatusForbidden, "forbidden", "missing permission "+perm)
		}
		c.Locals(claimsKey, claims)
		c.Locals(BillingCustomerIDKey, claims.CustomerID)
		c.Locals(BillingRoleKey, claims.Role)
		return c.Next()
	}
}

// BillingCustomerID returns the billing_customer_id local set by
// RequireWithBillingContext ("" when unset).
func BillingCustomerID(c fiber.Ctx) string {
	v, _ := c.Locals(BillingCustomerIDKey).(string)
	return v
}

// BillingRole returns the billing_role local set by
// RequireWithBillingContext ("" when unset).
func BillingRole(c fiber.Ctx) string {
	v, _ := c.Locals(BillingRoleKey).(string)
	return v
}
