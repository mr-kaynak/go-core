package service

import "time"

// EmailLogView is the identity module's neutral view of a notification email
// log. It carries exactly the fields the admin API serializes, with JSON tags
// identical to the notification module's EmailLog so the API contract is
// unchanged. Keeping this local avoids importing the notification module.
type EmailLogView struct {
	ID             string     `json:"id"`
	NotificationID *string    `json:"notification_id,omitempty"`
	From           string     `json:"from"`
	To             string     `json:"to"`
	CC             string     `json:"cc,omitempty"`
	BCC            string     `json:"bcc,omitempty"`
	Subject        string     `json:"subject"`
	Body           string     `json:"body"`
	Template       string     `json:"template,omitempty"`
	Status         string     `json:"status"`
	SMTPResponse   string     `json:"smtp_response,omitempty"`
	MessageID      string     `json:"message_id,omitempty"`
	Error          string     `json:"error,omitempty"`
	OpenedAt       *time.Time `json:"opened_at,omitempty"`
	ClickedAt      *time.Time `json:"clicked_at,omitempty"`
	BouncedAt      *time.Time `json:"bounced_at,omitempty"`
	UnsubscribedAt *time.Time `json:"unsubscribed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// NotificationReader is the narrow view of the notification repository that the
// admin service depends on. An adapter at the composition root wraps the
// concrete notification repository and converts email logs to EmailLogView,
// keeping the identity module free of a notification dependency.
type NotificationReader interface {
	CountByStatus() (map[string]int64, error)
	CountByType() (map[string]int64, error)
	ListEmailLogs(offset, limit int, status string) ([]*EmailLogView, int64, error)
}
