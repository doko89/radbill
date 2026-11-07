package infrastructure

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	notifyapp "boilerplate/api/internal/notify/application"
)

// smtpTimeout bounds the whole SMTP exchange so a dead server cannot stall
// background jobs.
const smtpTimeout = 10 * time.Second

// SMTPSender implements the smtp provider on the standard library.
type SMTPSender struct {
	cfg notifyapp.SMTPConfig
}

// NewSMTPSender builds an SMTPSender; NewDispatcher takes this constructor.
func NewSMTPSender(cfg notifyapp.SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

// Send delivers one message, dialing with a timeout. The context is observed
// only before dialing; net/smtp has no context-aware API.
func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if s.cfg.Host == "" {
		return fmt.Errorf("smtp not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	port := s.cfg.Port
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp hello: %w", err)
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.User != "" {
		auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	from := s.cfg.From
	if from == "" {
		from = s.cfg.User
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	msg := "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body
	if _, err := strings.NewReader(msg).WriteTo(w); err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return client.Quit()
}
