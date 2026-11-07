// Package infrastructure implements notify ports: log/smtp senders, the SQL
// log repository, and the settings snapshot adapter.
package infrastructure

import (
	"context"
	"log"
)

// LogSender implements the log provider: it records the message in the
// process log and always succeeds.
type LogSender struct{}

// Send logs the message.
func (LogSender) Send(_ context.Context, to, subject, body string) error {
	log.Printf("notify to=%s subject=%q body=%q", to, subject, body)
	return nil
}
