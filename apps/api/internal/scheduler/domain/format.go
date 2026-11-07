package domain

import (
	"strconv"
	"strings"
)

// FormatIDR renders amount with Indonesian thousand separators.
func FormatIDR(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}
	b.WriteString(s[:rem])
	for i := rem; i < len(s); i += 3 {
		b.WriteByte('.')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
