package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
)

var msg = Message{Event: "expiring", Database: "acme", Title: "La base acme expira en 3 días", Text: "línea 1\nlínea 2"}

func TestNoChannels(t *testing.T) {
	m := New(config.NotifyConfig{})
	assert.Empty(t, m.Channels())
	assert.ErrorIs(t, m.Send(context.Background(), msg), ErrNoChannels)
}

func TestWebhookPayloadWorksForDiscordAndSlack(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := New(config.NotifyConfig{WebhookURL: srv.URL})
	assert.Equal(t, []string{"webhook"}, m.Channels())
	require.NoError(t, m.Send(context.Background(), msg))
	assert.Contains(t, body["content"], "La base acme")
	assert.Equal(t, body["content"], body["text"])
	assert.Equal(t, "acme", body["database"])
}

func TestWebhookErrorsHideURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := (&Webhook{URL: srv.URL, Client: http.DefaultClient}).Send(context.Background(), msg)
	assert.ErrorContains(t, err, "status 500")

	secretURL := "http://127.0.0.1:1/secret-token"
	err = (&Webhook{URL: secretURL, Client: &http.Client{Timeout: time.Second}}).Send(context.Background(), msg)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-token")

	err = (&Webhook{URL: "::bad", Client: http.DefaultClient}).Send(context.Background(), msg)
	assert.Error(t, err)
}

func emailConfig() config.NotifyConfig {
	return config.NotifyConfig{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "u", SMTPPassword: "p",
		SMTPFrom: "posterndb@example.com", EmailTo: []string{"ops@example.com", "me@example.com"}}
}

func TestEmailComposition(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	e := &Email{cfg: emailConfig(), send: func(addr string, a smtp.Auth, from string, to []string, m []byte, _ string) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, m
		assert.NotNil(t, a)
		return nil
	}}
	require.NoError(t, e.Send(context.Background(), msg))
	assert.Equal(t, "smtp.example.com:587", gotAddr)
	assert.Equal(t, "posterndb@example.com", gotFrom)
	assert.Equal(t, []string{"ops@example.com", "me@example.com"}, gotTo)
	s := string(gotMsg)
	assert.Contains(t, s, "Subject: [PosternDB] La base acme expira en 3 días\r\n")
	assert.Contains(t, s, "línea 1\r\nlínea 2")
	assert.Equal(t, []string{"email"}, e.Channels())
}

func TestMultiJoinsErrors(t *testing.T) {
	cfg := emailConfig()
	cfg.WebhookURL = "http://127.0.0.1:1/x"
	m := New(cfg)
	assert.Equal(t, []string{"webhook", "email"}, m.Channels())
	m.senders[1] = &Email{cfg: cfg, send: func(string, smtp.Auth, string, []string, []byte, string) error {
		return errors.New("smtp down")
	}}
	err := m.Send(context.Background(), msg)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "webhook") && strings.Contains(err.Error(), "smtp down"))
}

func TestSMTPSendDialFailure(t *testing.T) {
	assert.Error(t, smtpSend("127.0.0.1:1", nil, "a@b", []string{"c@d"}, []byte("x"), "127.0.0.1"))
	assert.Error(t, smtpSend("127.0.0.1:465", nil, "a@b", []string{"c@d"}, []byte("x"), "127.0.0.1"))
}

// fakeSMTP speaks just enough SMTP for deliver().
func fakeSMTP(t *testing.T) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		defer ln.Close()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 fake")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					write("250 queued")
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 fake")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				write("250 ok")
			case cmd == "DATA":
				inData = true
				write("354 go")
			case cmd == "QUIT":
				write("221 bye")
				got <- data.String()
				return
			default:
				write("502 no")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestDeliver(t *testing.T) {
	addr, got := fakeSMTP(t)
	c, err := smtp.Dial(addr)
	require.NoError(t, err)
	require.NoError(t, deliver(c, nil, "a@example.com", []string{"b@example.com"}, compose("a@example.com", []string{"b@example.com"}, msg)))
	assert.Contains(t, <-got, "Subject: [PosternDB]")
}

func TestDeliverAuthUnsupported(t *testing.T) {
	addr, _ := fakeSMTP(t)
	c, err := smtp.Dial(addr)
	require.NoError(t, err)
	err = deliver(c, smtp.PlainAuth("", "u", "p", "127.0.0.1"), "a@example.com", []string{"b@example.com"}, []byte("x"))
	assert.Error(t, err)
}

func TestSMTPSendRequiresStartTLS(t *testing.T) {
	addr, _ := fakeSMTP(t)
	assert.Error(t, smtpSend(addr, nil, "a@b", []string{"c@d"}, []byte("x"), "127.0.0.1"))
}
