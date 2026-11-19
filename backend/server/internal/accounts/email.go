package accounts

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/mail"
)

// The sign-in email uses tables and inline styles only: that is what renders the same in Gmail,
// Outlook, and Apple Mail. The logo is the inline PNG part referenced by cid:. It never repeats the
// recipient's address: mail clients turn it into a link, and the recipient already knows it.
var loginEmailHTML = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>Your TaskPilot sign-in code</title>
<style>@media only screen and (max-width:480px){.tp-px{padding-left:24px!important;padding-right:24px!important}.tp-code{font-size:30px!important;letter-spacing:8px!important}}</style>
</head>
<body style="margin:0;padding:0;background-color:#f5f6f8;-webkit-text-size-adjust:100%;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">Use {{.Code}} to sign in to TaskPilot. The code expires in {{.Minutes}} minutes.</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#f5f6f8;">
<tr><td align="center" style="padding:48px 16px;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:480px;">
    <tr><td align="center" style="padding:0 0 24px 0;">
      <table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
        <td style="vertical-align:middle;"><img src="cid:{{.LogoCID}}" width="32" height="32" alt="" style="display:block;border:0;border-radius:8px;"></td>
        <td style="vertical-align:middle;padding-left:10px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:18px;font-weight:700;color:#111827;letter-spacing:-0.2px;">TaskPilot</td>
      </tr></table>
    </td></tr>
    <tr><td style="background-color:#ffffff;border:1px solid #e5e7eb;border-radius:12px;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
        <tr><td class="tp-px" align="center" style="padding:40px 40px 0 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
          <h1 style="margin:0 0 10px 0;font-size:22px;line-height:30px;font-weight:700;color:#111827;">Sign in to TaskPilot</h1>
          <p style="margin:0;font-size:15px;line-height:24px;color:#4b5563;">Enter this code on the sign-in page to continue.</p>
        </td></tr>
        <tr><td class="tp-px" align="center" style="padding:28px 40px;">
          <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="background-color:#f5f7ff;border:1px solid #e0e7ff;border-radius:10px;">
            <tr><td class="tp-code" style="padding:16px 18px 16px 30px;font-family:'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace;font-size:34px;line-height:40px;font-weight:700;letter-spacing:12px;color:#3730a3;text-align:center;">{{.Code}}</td></tr>
          </table>
          <p style="margin:14px 0 0 0;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;color:#6b7280;">Expires in {{.Minutes}} minutes · Can be used once</p>
        </td></tr>
        <tr><td class="tp-px" style="padding:0 40px;"><div style="height:1px;line-height:1px;font-size:0;background-color:#f0f1f3;">&nbsp;</div></td></tr>
        <tr><td class="tp-px" style="padding:20px 40px 32px 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:13px;line-height:20px;color:#6b7280;text-align:center;">
          If you didn't try to sign in, you can safely ignore this email.<br>Never share this code. TaskPilot will never ask you for it.
        </td></tr>
      </table>
    </td></tr>
    <tr><td style="padding:24px 0 0 0;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;color:#9ca3af;text-align:center;">
      TaskPilot · Background jobs and scheduling<br>
      This is an automated message. Please don't reply.
    </td></tr>
  </table>
</td></tr>
</table>
</body>
</html>
`))

// loginEmail is the sign-in code email in plain text and HTML.
func loginEmail(code string, minutes int) (mail.Message, error) {
	var html bytes.Buffer
	if err := loginEmailHTML.Execute(&html, struct {
		Code, LogoCID string
		Minutes       int
	}{code, mail.LogoCID, minutes}); err != nil {
		return mail.Message{}, fmt.Errorf("render sign-in email: %w", err)
	}
	text := strings.Join([]string{
		"Sign in to TaskPilot",
		"",
		"Your sign-in code: " + code,
		"",
		fmt.Sprintf("Enter it on the sign-in page to continue. It expires in %d minutes and can be used once.", minutes),
		"",
		"If you didn't try to sign in, you can safely ignore this email.",
		"Never share this code. TaskPilot will never ask you for it.",
		"",
		"TaskPilot · Background jobs and scheduling",
		"This is an automated message. Please don't reply.",
		"",
	}, "\n")
	return mail.Message{
		Subject: code + " is your TaskPilot sign-in code",
		Text:    text,
		HTML:    html.String(),
	}, nil
}
