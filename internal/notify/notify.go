// Package notify sends operator notifications (expiry warnings, expirations)
// through a webhook and/or email. Every channel is optional.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/tecsharp/posterndb/internal/config"
)

// Message is one notification.
type Message struct {
	Event    string `json:"event"`
	Database string `json:"database,omitempty"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

// Sender delivers notifications.
type Sender interface {
	Send(ctx context.Context, m Message) error
	Channels() []string
}

// ErrNoChannels is returned when no channel is configured.
var ErrNoChannels = errors.New("no hay canales de notificación configurados en .env")

// Multi fans a message out to every configured channel.
type Multi struct {
	senders []Sender
}

// New builds a sender from configuration.
func New(cfg config.NotifyConfig) *Multi {
	m := &Multi{}
	if cfg.WebhookURL != "" {
		m.senders = append(m.senders, &Webhook{URL: cfg.WebhookURL, Client: &http.Client{Timeout: 10 * time.Second}})
	}
	if cfg.EmailEnabled() {
		m.senders = append(m.senders, &Email{cfg: cfg, send: smtpSend})
	}
	return m
}

// Channels lists the configured channel names.
func (m *Multi) Channels() []string {
	out := []string{}
	for _, s := range m.senders {
		out = append(out, s.Channels()...)
	}
	return out
}

// Send delivers to every channel, returning the joined errors.
func (m *Multi) Send(ctx context.Context, msg Message) error {
	if len(m.senders) == 0 {
		return ErrNoChannels
	}
	var errs []error
	for _, s := range m.senders {
		if err := s.Send(ctx, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Webhook posts JSON. "content" is what Discord reads and "text" what Slack
// reads, so one payload works for both and for custom receivers.
type Webhook struct {
	URL    string
	Client *http.Client
}

// Channels names this channel.
func (w *Webhook) Channels() []string { return []string{"webhook"} }

// Send posts the message.
func (w *Webhook) Send(ctx context.Context, m Message) error {
	text := "**" + m.Title + "**\n" + m.Text
	body, err := json.Marshal(map[string]any{
		"content": text, "text": text, "event": m.Event, "database": m.Database, "title": m.Title,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		// The URL may embed a secret token (Discord, Slack); never log it.
		return errors.New("webhook: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: status %d", resp.StatusCode)
	}
	return nil
}

type smtpSendFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte, host string) error

// Email sends plain-text mail through SMTP with STARTTLS.
type Email struct {
	cfg  config.NotifyConfig
	send smtpSendFunc
}

// Channels names this channel.
func (e *Email) Channels() []string { return []string{"email"} }

// Send composes and sends the message.
func (e *Email) Send(_ context.Context, m Message) error {
	var auth smtp.Auth
	if e.cfg.SMTPUsername != "" {
		auth = smtp.PlainAuth("", e.cfg.SMTPUsername, e.cfg.SMTPPassword, e.cfg.SMTPHost)
	}
	addr := net.JoinHostPort(e.cfg.SMTPHost, strconv.Itoa(e.cfg.SMTPPort))
	return e.send(addr, auth, e.cfg.SMTPFrom, e.cfg.EmailTo, compose(e.cfg.SMTPFrom, e.cfg.EmailTo, m), e.cfg.SMTPHost)
}

func compose(from string, to []string, m Message) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, strings.Join(to, ", "), "[PosternDB] "+m.Title)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Text, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// smtpSend is net/smtp.SendMail with mandatory STARTTLS on port 587 and
// implicit TLS on port 465.
func smtpSend(addr string, a smtp.Auth, from string, to []string, msg []byte, host string) error {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var c *smtp.Client
	var err error
	if strings.HasSuffix(addr, ":465") {
		conn, dialErr := tls.Dial("tcp", addr, tlsCfg)
		if dialErr != nil {
			return dialErr
		}
		c, err = smtp.NewClient(conn, host)
	} else {
		c, err = smtp.Dial(addr)
		if err == nil {
			err = c.StartTLS(tlsCfg)
		}
	}
	if err != nil {
		return err
	}
	defer c.Close()
	return deliver(c, a, from, to, msg)
}

func deliver(c *smtp.Client, a smtp.Auth, from string, to []string, msg []byte) error {
	if a != nil {
		if err := c.Auth(a); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
