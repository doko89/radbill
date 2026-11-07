package kriskrisdomain

import "time"

// Unique-code bounds (PRD §6.4: 10..99 added to the transfer total).
const (
	MinUniqueCode int64 = 10
	MaxUniqueCode int64 = 99
)

// payment_attempts.status values (DB contract owned by C1).
const (
	AttemptPending   = "pending"
	AttemptPaid      = "paid"
	AttemptExpired   = "expired"
	AttemptCancelled = "cancelled"
	AttemptFailed    = "failed"
)

// invoices.status values relevant to payment (G1 contract).
const (
	InvoiceUnpaid = "unpaid"
	InvoicePaid   = "paid"
	InvoiceVoid   = "void"
)

// DefaultExtension is the PPPoE expiry extension when the package has no
// validity_seconds (mirrors billing DefaultManualExtension: 30 days).
const DefaultExtension = 30 * 24 * time.Hour

// QRValidity is the kriskris QR lifetime (PRD §6.4: 15 minutes).
const QRValidity = 15 * time.Minute

// MaxBatchIDs is the kriskris batch-status limit (PRD §6.4: max 50 ids).
const MaxBatchIDs = 50

// ValidUniqueCode reports whether code is in [10, 99].
func ValidUniqueCode(code int64) bool {
	return code >= MinUniqueCode && code <= MaxUniqueCode
}

// TotalWithUniqueCode returns base + code (pure amount math).
func TotalWithUniqueCode(base, code int64) int64 {
	return base + code
}

// ExtensionBase returns the time a validity extension starts from: the
// current expiry when still in the future, otherwise now.
func ExtensionBase(current *time.Time, now time.Time) time.Time {
	if current != nil && current.After(now) {
		return *current
	}
	return now
}

// ExtensionPeriod returns validity as a duration, or DefaultExtension when
// validity is nil/non-positive.
func ExtensionPeriod(validitySeconds *int64) time.Duration {
	if validitySeconds != nil && *validitySeconds > 0 {
		return time.Duration(*validitySeconds) * time.Second
	}
	return DefaultExtension
}
