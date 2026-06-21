// Package email provides SMTP email sending for the Master.
// Configuration is loaded from the database (smtp_config table, KEK-encrypted password).
// All Send* methods are no-ops when SMTP is disabled or not yet configured.
package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"

	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
)

// Mailer sends transactional emails using the SMTP config stored in the database.
type Mailer struct {
	q   *store.Queries
	kek []byte
}

// New creates a Mailer.
func New(q *store.Queries, kek []byte) *Mailer {
	return &Mailer{q: q, kek: kek}
}

// SendInvite sends a "set your password" invite email.
func (m *Mailer) SendInvite(ctx context.Context, to, setPasswordURL string) {
	subject := "You've been invited to orkestra"
	body := fmt.Sprintf(`Welcome to orkestra!

An administrator has created an account for you.
Click the link below to set your password and log in:

  %s

This link expires in 72 hours. If you did not expect this email, you can ignore it.
`, setPasswordURL)
	m.send(ctx, to, subject, body)
}

// SendPasswordReset sends a "reset your password" email.
func (m *Mailer) SendPasswordReset(ctx context.Context, to, resetURL string) {
	subject := "Reset your orkestra password"
	body := fmt.Sprintf(`You requested a password reset for your orkestra account.

Click the link below to set a new password:

  %s

This link expires in 1 hour. If you did not request a reset, you can ignore this email.
`, resetURL)
	m.send(ctx, to, subject, body)
}

// send loads the SMTP config from DB and delivers the message.
// It logs a warning and returns (without error) when SMTP is disabled or misconfigured,
// so callers (e.g. CreateUser) always succeed even without email.
func (m *Mailer) send(ctx context.Context, to, subject, body string) {
	cfg, err := m.q.GetSMTPConfig(ctx)
	if err != nil || !cfg.Enabled || cfg.Host == "" {
		slog.Warn("smtp not configured — skipping email", "to", to, "subject", subject)
		return
	}

	var password string
	if cfg.PasswordEnc != "" {
		enc, err := base64.StdEncoding.DecodeString(cfg.PasswordEnc)
		if err != nil {
			slog.Error("smtp: decode encrypted password", "err", err)
			return
		}
		dec, err := pki.Decrypt(m.kek, enc)
		if err != nil {
			slog.Error("smtp: decrypt password", "err", err)
			return
		}
		password = string(dec)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	from := cfg.FromAddress
	if from == "" {
		from = cfg.Username
	}

	msg := buildMessage(from, to, subject, body)

	if cfg.Starttls {
		if err := sendSTARTTLS(addr, cfg.Host, cfg.Username, password, from, to, msg); err != nil {
			slog.Error("smtp: send", "to", to, "err", err)
		}
	} else {
		auth := smtp.PlainAuth("", cfg.Username, password, cfg.Host)
		if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
			slog.Error("smtp: send", "to", to, "err", err)
		}
	}
}

func sendSTARTTLS(addr, host, user, password, from, to string, msg []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close() //nolint:errcheck

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if user != "" {
		auth := smtp.PlainAuth("", user, password, host)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func buildMessage(from, to, subject, body string) []byte {
	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n%s",
		from, to, subject, body,
	))
}
