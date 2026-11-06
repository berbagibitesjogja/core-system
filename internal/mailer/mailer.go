package mailer

import (
	"bytes"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/berbagibitesjogja/core-system/internal/config"
)

//go:embed templates/*.html
var templateFS embed.FS

var verificationTmpl = template.Must(template.ParseFS(templateFS, "templates/verification.html"))
var passwordResetTmpl = template.Must(template.ParseFS(templateFS, "templates/password_reset.html"))
var invitationTmpl = template.Must(template.ParseFS(templateFS, "templates/invitation.html"))

type Mailer struct {
	cfg config.SMTPConfig
}

type VerificationEmailData struct {
	RecipientName string
	Code          string
	ExpiryMinutes int
}

type PasswordResetEmailData struct {
	RecipientName string
	Code          string
	ExpiryMinutes int
}

type InvitationEmailData struct {
	RecipientName string
	InviterName   string
	Role          string
	DivisionName  string
	InviteLink    string
	ExpiryHours   int
}

func New(cfg config.SMTPConfig) *Mailer {
	return &Mailer{cfg: cfg}
}

func (m *Mailer) RenderInvitationEmail(data InvitationEmailData) (string, error) {
	var buf bytes.Buffer
	if err := invitationTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *Mailer) SendInvitationEmail(toEmail, recipientName, inviterName, role, divisionName, inviteLink string, expiryHours int) error {
	subject := "You have been invited to join Berbagi Bites Jogja"

	data := InvitationEmailData{
		RecipientName: recipientName,
		InviterName:   inviterName,
		Role:          role,
		DivisionName:  divisionName,
		InviteLink:    inviteLink,
		ExpiryHours:   expiryHours,
	}

	body, err := m.RenderInvitationEmail(data)
	if err != nil {
		return err
	}

	return m.sendEmail(toEmail, subject, body)
}

func (m *Mailer) SendVerificationEmail(toEmail, recipientName, code string, expiryMinutes int) error {
	subject := "Verify Your Email - Berbagi Bites Jogja"

	var buf bytes.Buffer
	data := VerificationEmailData{
		RecipientName: recipientName,
		Code:          code,
		ExpiryMinutes: expiryMinutes,
	}

	if err := verificationTmpl.Execute(&buf, data); err != nil {
		return err
	}

	return m.sendEmail(toEmail, subject, buf.String())
}

func (m *Mailer) SendPasswordResetEmail(toEmail, recipientName, code string, expiryMinutes int) error {
	subject := "Reset Your Password - Berbagi Bites Jogja"

	var buf bytes.Buffer
	data := PasswordResetEmailData{
		RecipientName: recipientName,
		Code:          code,
		ExpiryMinutes: expiryMinutes,
	}

	if err := passwordResetTmpl.Execute(&buf, data); err != nil {
		return err
	}

	return m.sendEmail(toEmail, subject, buf.String())
}

func (m *Mailer) sendEmail(toEmail, subject, htmlBody string) error {
	from := fmt.Sprintf("%s <%s>", m.cfg.FromName, m.cfg.FromEmail)
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = toEmail
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	var b strings.Builder
	for k, v := range headers {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(htmlBody)

	message := b.String()

	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))

	if !m.cfg.AuthRequired && !m.cfg.UseTLS {
		return smtp.SendMail(addr, nil, m.cfg.FromEmail, []string{toEmail}, []byte(message))
	}

	var auth smtp.Auth
	if m.cfg.AuthRequired && m.cfg.Username != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	if m.cfg.UseTLS {
		tlsConfig := &tls.Config{
			ServerName: m.cfg.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return m.sendSTARTTLS(addr, auth, toEmail, []byte(message))
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, m.cfg.Host)
		if err != nil {
			return err
		}
		defer client.Quit()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}

		if err := client.Mail(m.cfg.FromEmail); err != nil {
			return err
		}
		if err := client.Rcpt(toEmail); err != nil {
			return err
		}

		w, err := client.Data()
		if err != nil {
			return err
		}
		defer w.Close()

		_, err = w.Write([]byte(message))
		return err
	}

	return smtp.SendMail(addr, auth, m.cfg.FromEmail, []string{toEmail}, []byte(message))
}

func (m *Mailer) sendSTARTTLS(addr string, auth smtp.Auth, toEmail string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Quit()

	tlsConfig := &tls.Config{
		ServerName: m.cfg.Host,
	}

	if err := client.StartTLS(tlsConfig); err != nil {
		return err
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(m.cfg.FromEmail); err != nil {
		return err
	}
	if err := client.Rcpt(toEmail); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = w.Write(msg)
	return err
}
