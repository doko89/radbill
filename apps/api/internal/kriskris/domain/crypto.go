// Package kriskrisdomain holds pure kriskris payment logic: request
// canonicalization, HMAC signing/verification, amount math, and typed
// business errors. It imports stdlib only (no fiber/sql/os).
package kriskrisdomain

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"
)

// ClockTolerance is the max accepted skew between our clock and a kriskris
// timestamp (PRD §6.4: ±5 minutes; billing clock must run NTP).
const ClockTolerance = 5 * time.Minute

// BodyHashHex returns hex(sha256(body)). An empty/nil body hashes as the
// empty string (used for GET requests without a body).
func BodyHashHex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// CanonicalRequest builds the 5-line canonical string that kriskris request
// signatures are computed over (PRD §6.4):
//
//	timestamp + "\n" + METHOD + "\n" + path + "\n" + rawQuery + "\n" + hex(sha256(body))
//
// timestamp is RFC3339 (UTC), method is uppercased, path is the request path
// without host (e.g. "/api/v1/external/invoices"), rawQuery is the raw
// query string without "?" ("" when absent).
func CanonicalRequest(timestamp, method, path, rawQuery string, body []byte) string {
	var b strings.Builder
	b.Grow(len(timestamp) + len(method) + len(path) + len(rawQuery) + 64 + 4)
	b.WriteString(timestamp)
	b.WriteByte('\n')
	b.WriteString(strings.ToUpper(method))
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString(rawQuery)
	b.WriteByte('\n')
	b.WriteString(BodyHashHex(body))
	return b.String()
}

// SignRequest returns hex(HMAC-SHA256(secret, canonical)).
func SignRequest(secret, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature checks the callback HMAC over the raw request body.
// header must be "sha256=<hex>" (PRD §6.4); comparison is constant-time.
func VerifyWebhookSignature(callbackSecret string, rawBody []byte, header string) bool {
	if callbackSecret == "" || header == "" {
		return false
	}
	hexSig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || hexSig == "" {
		return false
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(callbackSecret))
	mac.Write(rawBody)
	want := mac.Sum(nil)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// TimestampFresh reports whether ts is within ±ClockTolerance of now.
func TimestampFresh(ts, now time.Time) bool {
	d := now.Sub(ts)
	if d < 0 {
		d = -d
	}
	return d <= ClockTolerance
}

// ParseTimestamp parses an RFC3339 timestamp header value.
func ParseTimestamp(v string) (time.Time, error) {
	return time.Parse(time.RFC3339, v)
}
