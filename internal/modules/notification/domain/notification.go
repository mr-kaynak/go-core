package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeEmail   NotificationType = "email"
	NotificationTypeSMS     NotificationType = "sms"
	NotificationTypePush    NotificationType = "push"
	NotificationTypeInApp   NotificationType = "in_app"
	NotificationTypeWebhook NotificationType = "webhook"
)

// NotificationStatus represents the status of a notification
type NotificationStatus string

const (
	NotificationStatusPending    NotificationStatus = "pending"
	NotificationStatusProcessing NotificationStatus = "processing"
	NotificationStatusSent       NotificationStatus = "sent"
	NotificationStatusFailed     NotificationStatus = "failed"
	NotificationStatusCanceled   NotificationStatus = "canceled"
	NotificationStatusBounced    NotificationStatus = "bounced"
	NotificationStatusRead       NotificationStatus = "read"
)

// NotificationPriority represents the priority of a notification
type NotificationPriority string

const (
	NotificationPriorityLow    NotificationPriority = "low"
	NotificationPriorityNormal NotificationPriority = "normal"
	NotificationPriorityHigh   NotificationPriority = "high"
	NotificationPriorityUrgent NotificationPriority = "urgent"
)

// Notification represents a notification record
type Notification struct {
	ID          uuid.UUID            `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID      uuid.UUID            `gorm:"type:uuid;index" json:"user_id"`
	Type        NotificationType     `gorm:"type:varchar(20);not null" json:"type"`
	Status      NotificationStatus   `gorm:"type:varchar(20);default:'pending'" json:"status"`
	Priority    NotificationPriority `gorm:"type:varchar(20);default:'normal'" json:"priority"`
	Subject     string               `json:"subject"`
	Content     string               `gorm:"type:text" json:"content"`
	Template    string               `json:"template,omitempty"`
	Recipients  string               `json:"recipients"` // JSON array of recipients
	Metadata    string               `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	ScheduledAt *time.Time           `json:"scheduled_at,omitempty"`
	SentAt      *time.Time           `json:"sent_at,omitempty"`
	FailedAt    *time.Time           `json:"failed_at,omitempty"`
	Error       string               `json:"error,omitempty"`
	RetryCount  int                  `gorm:"default:0" json:"retry_count"`
	MaxRetries  int                  `gorm:"default:3" json:"max_retries"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	DeletedAt   gorm.DeletedAt       `gorm:"index" json:"-"`
}

// EmailLog represents an email sending log
type EmailLog struct {
	ID             uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NotificationID *uuid.UUID `gorm:"type:uuid;index" json:"notification_id,omitempty"`
	From           string     `gorm:"not null" json:"from"`
	To             string     `gorm:"not null" json:"to"`
	CC             string     `json:"cc,omitempty"`
	BCC            string     `json:"bcc,omitempty"`
	Subject        string     `gorm:"not null" json:"subject"`
	Body           string     `gorm:"type:text" json:"body"`
	Template       string     `json:"template,omitempty"`
	Status         string     `gorm:"type:varchar(20);default:'pending'" json:"status"`
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

// NotificationTemplate represents a notification template
type NotificationTemplate struct {
	ID          uuid.UUID        `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string           `gorm:"uniqueIndex;not null" json:"name"`
	Type        NotificationType `gorm:"type:varchar(20);not null" json:"type"`
	Subject     string           `json:"subject,omitempty"`
	Body        string           `gorm:"type:text" json:"body"`
	Variables   string           `gorm:"type:jsonb;default:'[]'" json:"variables,omitempty"` // JSON array of required variables
	IsActive    bool             `gorm:"default:true" json:"is_active"`
	Description string           `json:"description,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	DeletedAt   gorm.DeletedAt   `gorm:"index" json:"-"`
}

// NotificationPreference represents user notification preferences
type NotificationPreference struct {
	ID                 uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID             uuid.UUID      `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`
	EmailEnabled       bool           `gorm:"default:true" json:"email_enabled"`
	SMSEnabled         bool           `gorm:"default:false" json:"sms_enabled"`
	PushEnabled        bool           `gorm:"default:false" json:"push_enabled"`
	InAppEnabled       bool           `gorm:"default:true" json:"in_app_enabled"`
	EmailFrequency     string         `gorm:"type:varchar(20);default:'immediate'" json:"email_frequency"` // immediate, daily, weekly
	UnsubscribedTopics string         `gorm:"type:jsonb;default:'[]'" json:"unsubscribed_topics,omitempty"` // JSON array of topics
	QuietHoursStart    *time.Time     `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd      *time.Time     `json:"quiet_hours_end,omitempty"`
	Timezone           string         `gorm:"default:'UTC'" json:"timezone"`
	Language           string         `gorm:"default:'en'" json:"language"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for Notification
func (Notification) TableName() string {
	return "notifications"
}

// TableName specifies the table name for EmailLog
func (EmailLog) TableName() string {
	return "email_logs"
}

// TableName specifies the table name for NotificationTemplate
func (NotificationTemplate) TableName() string {
	return "notification_templates"
}

// TableName specifies the table name for NotificationPreference
func (NotificationPreference) TableName() string {
	return "notification_preferences"
}

// BeforeCreate hook for Notification
func (n *Notification) BeforeCreate(tx *gorm.DB) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	return nil
}

// IsPending checks if notification is pending
func (n *Notification) IsPending() bool {
	return n.Status == NotificationStatusPending
}

// IsScheduled checks if notification is scheduled
func (n *Notification) IsScheduled() bool {
	return n.ScheduledAt != nil && n.ScheduledAt.After(time.Now())
}

// CanRetry checks if notification can be retried
func (n *Notification) CanRetry() bool {
	return n.Status == NotificationStatusFailed && n.RetryCount < n.MaxRetries
}

// IncrementRetry increments the retry count
func (n *Notification) IncrementRetry() {
	n.RetryCount++
}

// MarkAsSent marks the notification as sent
func (n *Notification) MarkAsSent() {
	now := time.Now()
	n.Status = NotificationStatusSent
	n.SentAt = &now
}

// MarkAsRead marks the notification as read
func (n *Notification) MarkAsRead() {
	n.Status = NotificationStatusRead
}

// MarkAsFailed marks the notification as failed
func (n *Notification) MarkAsFailed(err error) {
	now := time.Now()
	n.Status = NotificationStatusFailed
	n.FailedAt = &now
	if err != nil {
		n.Error = err.Error()
	}
}
