package kriskrisdomain

import "errors"

// kriskris business error codes (PRD §6.4 table + rate limit guard).
const (
	CodeDuplicatePending = "duplicate_pending"
	CodeGobizOffline     = "gobiz_offline"
	CodeQuotaExceeded    = "quota_exceeded"
	CodeInvalidAmount    = "invalid_amount"
	// CodeRateLimited is synthesized locally for HTTP 429 (the guide
	// carries data.retry_after; callers back off and retry).
	CodeRateLimited = "rate_limited"
)

// BizError is a typed kriskris business error. Callers switch on Code:
// duplicate_pending → short retry; gobiz_offline/quota_exceeded → hide the
// pay button and offer manual payment; invalid_amount → log, generic message.
type BizError struct {
	Code    string
	Message string
}

// Error implements error.
func (e *BizError) Error() string {
	if e.Message == "" {
		return "kriskris: " + e.Code
	}
	return "kriskris: " + e.Code + ": " + e.Message
}

// NewBizError builds a *BizError (nil when code is "").
func NewBizError(code, message string) *BizError {
	if code == "" {
		return nil
	}
	return &BizError{Code: code, Message: message}
}

// BizCode returns the business code carried by err, or "" when err is not a
// *BizError (works through wrapping via errors.As).
func BizCode(err error) string {
	var be *BizError
	if errors.As(err, &be) {
		return be.Code
	}
	return ""
}

// IsBizCode reports whether err carries the given business code.
func IsBizCode(err error, code string) bool {
	return BizCode(err) == code && code != ""
}
