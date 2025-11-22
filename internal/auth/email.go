package auth

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"
	"text/template"
	"time"

	"github.com/rs/zerolog"
)

// EmailService handles sending emails via SMTP.
type EmailService struct {
	smtpHost     string
	smtpPort     int
	smtpUsername string
	smtpPassword string
	fromEmail    string
	logger       zerolog.Logger
}

// EmailConfig holds SMTP configuration.
type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	FromEmail    string
}

// NewEmailService creates an email service.
func NewEmailService(cfg EmailConfig, logger zerolog.Logger) *EmailService {
	return &EmailService{
		smtpHost:     cfg.SMTPHost,
		smtpPort:     cfg.SMTPPort,
		smtpUsername: cfg.SMTPUsername,
		smtpPassword: cfg.SMTPPassword,
		fromEmail:    cfg.FromEmail,
		logger:       logger.With().Str("component", "email").Logger(),
	}
}

// SendPasswordResetEmail sends a password reset email with the reset token.
func (e *EmailService) SendPasswordResetEmail(ctx context.Context, toEmail, resetToken string) error {
	if e.smtpHost == "" || e.smtpPort == 0 {
		return fmt.Errorf("email service not configured")
	}

	resetURL := fmt.Sprintf("https://your-app.com/reset-password?token=%s", resetToken)
	
	tmpl := `Subject: Password Reset Request

Hello,

You requested a password reset for your account.

Click the link below to reset your password:
{{.ResetURL}}

This link will expire in 1 hour.

If you did not request this, please ignore this email.

Best regards,
Quiz Platform Team`

	t, err := template.New("reset").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var body bytes.Buffer
	if err := t.Execute(&body, map[string]string{
		"ResetURL": resetURL,
	}); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", e.smtpHost, e.smtpPort)
	auth := smtp.PlainAuth("", e.smtpUsername, e.smtpPassword, e.smtpHost)

	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\n%s\r\n", e.fromEmail, toEmail, body.String()))

	if err := smtp.SendMail(addr, auth, e.fromEmail, []string{toEmail}, msg); err != nil {
		e.logger.Error().Err(err).Str("to", toEmail).Msg("failed to send password reset email")
		return fmt.Errorf("send email: %w", err)
	}

	e.logger.Info().Str("to", toEmail).Msg("password reset email sent")
	return nil
}

// PasswordResetToken represents a password reset token stored in Redis.
type PasswordResetToken struct {
	UserID    string
	Email     string
	ExpiresAt time.Time
}

