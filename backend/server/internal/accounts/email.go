package accounts

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/mail"
)

// The sign-in email uses tables and inline styles only: that is what renders the same in Gmail,
// Outlook, and Apple Mail. The logo is the inline PNG part referenced by cid:.
var loginEmailHTML = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>Your TaskPilot sign-in code</title>
<style>@media only screen and (max-width:480px){.tp-px{padding-left:24px!important;padding-right:24px!important}.tp-code{font-size:28px!important;letter-spacing:6px!important;padding:14px 18px 14px 24px!important}}</style>
</head>
<body style="margin:0;padding:0;background-color:#f4f4f7;-webkit-text-size-adjust:100%;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">Your code is {{.Code}}. It expires in {{.Minutes}} minutes.</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#f4f4f7;">
<tr><td align="center" style="padding:40px 16px;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:520px;">
    <tr><td style="padding:0 4px 20px 4px;">
      <table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
        <td style="vertical-align:middle;"><img src="cid:{{.LogoCID}}" width="36" height="36" alt="TaskPilot" style="display:block;border:0;border-radius:9px;"></td>
        <td style="vertical-align:middle;padding-left:10px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:19px;font-weight:700;color:#111827;letter-spacing:-0.2px;">TaskPilot</td>
      </tr></table>
    </td></tr>
    <tr><td style="background-color:#ffffff;border:1px solid #e5e7eb;border-radius:14px;overflow:hidden;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
        <tr><td style="height:4px;line-height:4px;font-size:0;background-color:#4f46e5;">&nbsp;</td></tr>
        <tr><td class="tp-px" style="padding:36px 40px 8px 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
          <h1 style="margin:0 0 12px 0;font-size:22px;line-height:30px;font-weight:700;color:#111827;">Your sign-in code</h1>
          <p style="margin:0;font-size:15px;line-height:24px;color:#4b5563;">Enter this code on the TaskPilot sign-in page to finish signing in as <strong style="color:#111827;">{{.Email}}</strong>.</p>
        </td></tr>
        <tr><td class="tp-px" align="center" style="padding:24px 40px;">
          <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="background-color:#eef2ff;border:1px solid #c7d2fe;border-radius:12px;">
            <tr><td class="tp-code" style="padding:18px 22px 18px 32px;font-family:'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace;font-size:34px;line-height:40px;font-weight:700;letter-spacing:10px;color:#312e81;text-align:center;">{{.Code}}</td></tr>
          </table>
        </td></tr>
        <tr><td class="tp-px" style="padding:0 40px 28px 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:22px;color:#4b5563;text-align:center;">
          This code expires in <strong style="color:#111827;">{{.Minutes}} minutes</strong> and can be used only once.
        </td></tr>
        <tr><td class="tp-px" style="padding:0 40px;"><div style="height:1px;line-height:1px;font-size:0;background-color:#e5e7eb;">&nbsp;</div></td></tr>
        <tr><td class="tp-px" style="padding:22px 40px 32px 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;color:#6b7280;">
          <strong style="color:#374151;">Didn't request this?</strong> You can safely ignore this email. Someone may have typed your address by mistake, and nobody can sign in without this code. TaskPilot will never ask you to share it.
        </td></tr>
      </table>
    </td></tr>
    <tr><td style="padding:22px 4px 0 4px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;color:#9ca3af;text-align:center;">
      TaskPilot · Background jobs and scheduling for any application<br>
      You received this email because a sign-in was requested for {{.Email}}.
    </td></tr>
  </table>
</td></tr>
</table>
</body>
</html>
`))

// loginEmail is the sign-in code email in plain text and HTML.
func loginEmail(email, code string, minutes int) (mail.Message, error) {
	var html bytes.Buffer
	if err := loginEmailHTML.Execute(&html, struct {
		Email, Code, LogoCID string
		Minutes              int
	}{email, code, mail.LogoCID, minutes}); err != nil {
		return mail.Message{}, fmt.Errorf("render sign-in email: %w", err)
	}
	text := strings.Join([]string{
		"TaskPilot",
		"",
		"Your sign-in code is " + code,
		"",
		fmt.Sprintf("Enter it on the TaskPilot sign-in page to finish signing in as %s.", email),
		fmt.Sprintf("It expires in %d minutes and can be used only once.", minutes),
		"",
		"Didn't request this? You can safely ignore this email. Nobody can sign in without this code,",
		"and TaskPilot will never ask you to share it.",
		"",
	}, "\n")
	return mail.Message{
		Subject: code + " is your TaskPilot sign-in code",
		Text:    text,
		HTML:    html.String(),
	}, nil
}
