package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeSMTP accepts one message and records the envelope sender and the data.
func fakeSMTP(t *testing.T) (port int, got chan [2]string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	got = make(chan [2]string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var from, data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				from.WriteString(strings.TrimSpace(line[len("MAIL FROM:"):]))
				say("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				say("250 ok")
			case cmd == "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(strings.TrimPrefix(l, ".")) // undo dot-stuffing
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				got <- [2]string{from.String(), data.String()}
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

var msg = Message{Subject: "123456 is your code", Text: "Code 123456\nBye", HTML: `<p>Code <b>123456</b></p><img src="cid:taskpilot-logo">`}

func TestSMTP_SendsMultipartWithLogo(t *testing.T) {
	port, got := fakeSMTP(t)
	s := SMTP{Host: "127.0.0.1", Port: port, From: "no-reply@example.com"}
	require.NoError(t, s.Send(context.Background(), "dev@example.com", msg))
	sent := <-got
	require.Equal(t, "<no-reply@example.com>", sent[0], "the envelope uses the bare address")

	m, err := netmail.ReadMessage(strings.NewReader(sent[1]))
	require.NoError(t, err)
	from, err := netmail.ParseAddress(m.Header.Get("From"))
	require.NoError(t, err)
	require.Equal(t, "TaskPilot", from.Name, "a plain From address gets the TaskPilot display name")
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, msg.Subject, subject)

	// multipart/alternative: text/plain, then multipart/related with the HTML and the logo.
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	require.NoError(t, err)
	alt := multipart.NewReader(m.Body, params["boundary"])
	text, err := alt.NextPart()
	require.NoError(t, err)
	body, _ := io.ReadAll(quotedprintable.NewReader(text))
	require.Equal(t, "Code 123456\r\nBye", string(body))

	related, err := alt.NextPart()
	require.NoError(t, err)
	mt, rp, err := mime.ParseMediaType(related.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/related", mt)
	rel := multipart.NewReader(related, rp["boundary"])
	html, err := rel.NextPart()
	require.NoError(t, err)
	hb, _ := io.ReadAll(quotedprintable.NewReader(html))
	require.Equal(t, msg.HTML, string(hb))
	logo, err := rel.NextPart()
	require.NoError(t, err)
	require.Equal(t, "<taskpilot-logo>", logo.Header.Get("Content-Id"))
	raw, _ := io.ReadAll(logo)
	png, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(raw), "\r\n", ""))
	require.NoError(t, err)
	require.True(t, bytes.Equal(logoPNG, png), "the logo arrives intact")
}

func TestBuild_PlainTextOnly(t *testing.T) {
	out, err := build("TaskPilot <a@b.c>", "d@e.f", Message{Subject: "s", Text: "hi"}, time.Unix(0, 0))
	require.NoError(t, err)
	require.Contains(t, string(out), "Content-Type: text/plain; charset=utf-8")
	require.NotContains(t, string(out), "multipart")
}

func TestSMTP_RejectsHeaderInjection(t *testing.T) {
	s := SMTP{Host: "127.0.0.1", Port: 1, From: "no-reply@example.com"}
	require.Error(t, s.Send(context.Background(), "dev@example.com\r\nBcc: x@y.z", msg))
	require.Error(t, SMTP{Host: "127.0.0.1", Port: 1, From: "not an address"}.Send(context.Background(), "a@b.c", msg))
}
