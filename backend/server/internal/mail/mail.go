// Package mail sends the dashboard's sign-in emails. SMTP uses only the standard library.
package mail

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// LogoCID is the Content-ID of the TaskPilot logo attached to HTML emails: reference it as
// <img src="cid:taskpilot-logo">. Gmail and most clients block SVG and data: images, so the logo is a
// small inline PNG part instead.
const LogoCID = "taskpilot-logo"

//go:embed logo.png
var logoPNG []byte

// defaultFromName is shown as the sender when TASKPILOT_SMTP_FROM is a plain address.
const defaultFromName = "TaskPilot"

// Message is one email: a plain-text body for every client and an optional HTML body.
type Message struct {
	Subject string
	Text    string
	HTML    string // may reference cid:taskpilot-logo
}

// Sender delivers one email.
type Sender interface {
	Send(ctx context.Context, to string, m Message) error
}

// SMTP sends through an SMTP server. net/smtp upgrades to TLS with STARTTLS when the server offers it,
// and refuses to send the password over an unencrypted connection to anything but localhost.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Send delivers the message.
func (s SMTP) Send(_ context.Context, to string, m Message) error {
	if strings.ContainsAny(to+m.Subject, "\r\n") {
		return fmt.Errorf("send mail: header contains a line break")
	}
	// From may carry a display name ("TaskPilot <no-reply@example.com>"); the envelope needs the bare address.
	from, err := netmail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("send mail: invalid from address: %w", err)
	}
	if from.Name == "" {
		from.Name = defaultFromName
	}
	msg, err := build(from.String(), to, m, time.Now())
	if err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	if err := smtp.SendMail(addr, auth, from.Address, []string{to}, msg); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}

// build encodes the message as MIME. With HTML it is
//
//	multipart/alternative
//	├─ text/plain
//	└─ multipart/related
//	   ├─ text/html
//	   └─ image/png (the logo, Content-ID taskpilot-logo)
func build(from, to string, m Message, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	header := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	header("From", from)
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.UTC().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")

	if m.HTML == "" {
		header("Content-Type", "text/plain; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		return buf.Bytes(), writeQP(&buf, m.Text)
	}

	alt := multipart.NewWriter(&buf)
	header("Content-Type", `multipart/alternative; boundary="`+alt.Boundary()+`"`)
	buf.WriteString("\r\n")

	text, err := alt.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	if err := writeQP(text, m.Text); err != nil {
		return nil, err
	}

	var relBuf bytes.Buffer
	rel := multipart.NewWriter(&relBuf)
	html, err := rel.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	if err := writeQP(html, m.HTML); err != nil {
		return nil, err
	}
	logo, err := rel.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`image/png; name="taskpilot.png"`},
		"Content-Transfer-Encoding": {"base64"},
		"Content-ID":                {"<" + LogoCID + ">"},
		"Content-Disposition":       {`inline; filename="taskpilot.png"`},
	})
	if err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(logoPNG)
	for len(enc) > 76 {
		_, _ = logo.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	_, _ = logo.Write([]byte(enc + "\r\n"))
	if err := rel.Close(); err != nil {
		return nil, err
	}

	related, err := alt.CreatePart(textproto.MIMEHeader{
		"Content-Type": {`multipart/related; type="text/html"; boundary="` + rel.Boundary() + `"`},
	})
	if err != nil {
		return nil, err
	}
	if _, err := related.Write(relBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := alt.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeQP(w interface{ Write([]byte) (int, error) }, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(strings.ReplaceAll(s, "\n", "\r\n"))); err != nil {
		return err
	}
	return qp.Close()
}

// Log writes the email to the log instead of sending it. Development only: it lets local setup sign in
// without an SMTP server (the code appears in the API's output). Only the plain-text body is logged.
type Log struct{ Logger *slog.Logger }

// Send logs the message.
func (l Log) Send(ctx context.Context, to string, m Message) error {
	l.Logger.WarnContext(ctx, "email not sent (SMTP not configured, development only)",
		"to", to, "subject", m.Subject, "body", m.Text)
	return nil
}
