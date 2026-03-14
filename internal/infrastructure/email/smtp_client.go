package email

import (
	"fmt"

	mail "github.com/wneessen/go-mail"

	"github.com/mr-kaynak/go-core/internal/core/config"
)

// NewSMTPClient creates a new SMTP mail client configured from the application config.
// TLS policy is determined by port and environment:
//   - Port 25 or 1025: no TLS (local/test mail servers)
//   - Development environment: opportunistic TLS
//   - Otherwise: mandatory TLS
//
// SMTP authentication is enabled when SMTPUser is configured.
func NewSMTPClient(cfg *config.Config) (*mail.Client, error) {
	var tlsPolicy mail.TLSPolicy
	switch {
	case cfg.Email.SMTPPort == 25 || cfg.Email.SMTPPort == 1025:
		tlsPolicy = mail.NoTLS
	case cfg.IsDevelopment():
		tlsPolicy = mail.TLSOpportunistic
	default:
		tlsPolicy = mail.TLSMandatory
	}

	opts := []mail.Option{
		mail.WithPort(cfg.Email.SMTPPort),
		mail.WithTLSPolicy(tlsPolicy),
	}
	if cfg.Email.SMTPUser != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.Email.SMTPUser),
			mail.WithPassword(cfg.Email.SMTPPassword))
	}

	client, err := mail.NewClient(cfg.Email.SMTPHost, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create mail client: %w", err)
	}

	return client, nil
}
