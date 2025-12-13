# Notification System - Quick Reference Guide

## Core Files Location Map

### Domain Models
- `/internal/modules/notification/domain/notification.go` - Notification, NotificationTemplate, NotificationPreference, EmailLog
- `/internal/modules/notification/domain/template.go` - ExtendedNotificationTemplate, TemplateLanguage, TemplateVariable, TemplateCategory

### Services
- `/internal/modules/notification/service/notification_service.go` - Main notification orchestration
- `/internal/modules/notification/service/template_service.go` - Template management & rendering
- `/internal/modules/notification/service/enhanced_email_service.go` - Database-backed email with templates
- `/internal/infrastructure/email/email_service.go` - Base SMTP email service

### Repositories
- `/internal/modules/notification/repository/notification_repository.go` - Interface definitions
- `/internal/modules/notification/repository/notification_repository_impl.go` - Database implementation
- `/internal/modules/notification/repository/template_repository.go` - Template CRUD & queries

### API Handlers
- `/internal/modules/notification/api/template_handler.go` - HTTP endpoints for template management

### Event System
- `/internal/infrastructure/messaging/events/event_dispatcher.go` - Event dispatching
- `/internal/infrastructure/messaging/rabbitmq/rabbitmq_service.go` - RabbitMQ integration
- `/internal/infrastructure/messaging/domain/outbox.go` - Transactional outbox pattern

---

## Quick Code Examples

### 1. Send Email Notification
```go
req := &service.SendEmailRequest{
    UserID:      userID,
    To:          []string{"user@example.com"},
    Subject:     "Welcome!",
    Template:    "welcome_user",
    Data: map[string]interface{}{
        "Username": "John",
        "AppName":  "MyApp",
        "LoginURL": "https://app.com/login",
    },
    Priority: "high",
}

notification, err := notificationService.SendEmail(req)
// Returns immediately with PENDING notification
// Actual send happens in background goroutine
```

### 2. Send Generic Notification
```go
req := &service.SendNotificationRequest{
    UserID:     userID,
    Type:       "sms",  // email, sms, push, in_app, webhook
    Priority:   "normal",
    Subject:    "SMS Title",
    Content:    "Message body",
    Recipients: []string{phoneNumber},
    Metadata: map[string]interface{}{
        "phone": phoneNumber,
    },
}

notification, err := notificationService.SendNotification(req)
```

### 3. Create Custom Template
```go
req := &service.CreateTemplateRequest{
    Name:     "order_confirmation",
    Type:     "email",
    Subject:  "Order {{.OrderID}} Confirmed",
    Body:     "Thank you {{.CustomerName}}! Your order for {{.Amount}} has been confirmed.",
    Variables: []service.VariableRequest{
        {Name: "OrderID", Type: "string", Required: true},
        {Name: "CustomerName", Type: "string", Required: true},
        {Name: "Amount", Type: "string", Required: true},
    },
    Languages: []service.LanguageVariantRequest{
        {
            LanguageCode: "en",
            Subject:      "Order {{.OrderID}} Confirmed",
            Body:         "Thank you {{.CustomerName}}!",
            IsDefault:    true,
        },
    },
    Tags:     []string{"order", "confirmation"},
    IsActive: true,
}

template, err := templateService.CreateTemplate(req)
```

### 4. Render Template
```go
req := &service.RenderTemplateRequest{
    TemplateName: "welcome_user",
    LanguageCode: "en",
    Data: map[string]interface{}{
        "Username": "Alice",
        "AppName":  "CoolApp",
        "LoginURL": "https://cool.app/login",
    },
}

rendered, err := templateService.RenderTemplate(req)
// rendered.Subject = "Welcome to CoolApp!"
// rendered.Body = "Hello Alice! Welcome to CoolApp! To get started, visit: https://cool.app/login..."
```

### 5. Update User Preferences
```go
pref := &domain.NotificationPreference{
    UserID:         userID,
    EmailEnabled:   true,
    SMSEnabled:     false,
    PushEnabled:    true,
    InAppEnabled:   true,
    EmailFrequency: "immediate",
    Timezone:       "America/New_York",
    Language:       "en",
}

err := notificationService.UpdateUserPreferences(userID, pref)
```

### 6. Process Pending Notifications (Background Job)
```go
// Call periodically (e.g., every 5 seconds via cron)
err := notificationService.ProcessPendingNotifications(100)
// Processes up to 100 PENDING notifications
// Sends each in a goroutine
// Updates status to SENT or FAILED
```

### 7. Retry Failed Notifications (Background Job)
```go
// Call periodically (e.g., every minute via cron)
err := notificationService.RetryFailedNotifications(50)
// Finds FAILED notifications with retry_count < max_retries
// Increments retry_count and resets to PENDING
// Spawns goroutine to resend
```

### 8. Dispatch Event
```go
event := &events.DomainEvent{
    Type:          events.EventUserRegistered,
    AggregateID:   userID.String(),
    AggregateType: "User",
    UserID:        userID.String(),
    Data: map[string]interface{}{
        "email":    "user@example.com",
        "username": "john",
    },
}

err := eventDispatcher.Dispatch(ctx, event)
// Saves to OutboxMessage table
// Background process publishes to RabbitMQ
// Triggers local handlers
```

---

## Notification Status Flow

```
PENDING → PROCESSING → SENT ✓
   ↓          ↓
 FAILED (retry_count < max_retries)
   ↓
 PERMANENT FAILURE (retry_count >= max_retries)

CANCELLED (user action)
BOUNCED (email provider)
```

---

## Template Variables - Go Syntax

Templates use standard Go `html/template` syntax with custom functions:

```
{{.VariableName}}              - Basic substitution
{{.VariableName | upper}}      - Uppercase
{{.VariableName | lower}}      - Lowercase
{{.VariableName | title}}      - Title case
{{.VariableName | capitalize}} - First letter cap
{{.VariableName | trim}}       - Remove whitespace
{{if .Condition}}...{{end}}    - Conditionals
{{range .Items}}...{{end}}     - Loops
{{.Items | pluralize 1 "item" "items"}} - Pluralize
{{.Value | default "N/A"}}     - Default value
```

---

## Database Queries

### Get user's notifications
```sql
SELECT * FROM notifications 
WHERE user_id = $1 
ORDER BY created_at DESC 
LIMIT 20;
```

### Get pending notifications
```sql
SELECT * FROM notifications 
WHERE status = 'pending' 
  AND (scheduled_at IS NULL OR scheduled_at <= NOW())
ORDER BY priority DESC, created_at ASC 
LIMIT 100;
```

### Get failed notifications for retry
```sql
SELECT * FROM notifications 
WHERE status = 'failed' 
  AND retry_count < max_retries
ORDER BY priority DESC, created_at ASC 
LIMIT 50;
```

### Get template by name with relationships
```sql
SELECT * FROM notification_templates t
LEFT JOIN template_languages tl ON t.id = tl.template_id
LEFT JOIN template_variables tv ON t.id = tv.template_id
WHERE t.name = 'user_verification';
```

### Get outbox messages pending publication
```sql
SELECT * FROM outbox_messages 
WHERE status = 'pending' 
ORDER BY priority DESC, created_at ASC 
LIMIT 10;
```

---

## Environment Configuration Needed

```env
# SMTP
EMAIL_SMTP_HOST=smtp.gmail.com
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USER=notifications@app.com
EMAIL_SMTP_PASSWORD=xxxx
EMAIL_FROM_NAME=MyApp
EMAIL_FROM_EMAIL=noreply@app.com

# RabbitMQ
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
RABBITMQ_EXCHANGE=go-core

# Database (PostgreSQL)
DATABASE_URL=postgres://user:pass@localhost:5432/db

# API
APP_BASE_URL=https://app.com
APP_NAME=MyApplication
```

---

## Key Metrics to Monitor

1. **Notification Creation Rate** - New notifications per second
2. **Delivery Success Rate** - % of SENT vs FAILED
3. **Average Delivery Time** - Created to SENT duration
4. **Retry Rate** - Failed retries / total
5. **Outbox Backlog** - Pending messages in outbox
6. **Template Usage** - Most used templates
7. **DLQ Count** - Dead lettered messages (errors)
8. **SMTP Response Times** - Email send latency
9. **Email Open Rate** - For email_logs opened_at
10. **Bounce Rate** - For email_logs bounced_at

---

## Common Troubleshooting

**Issue: Notifications not sending**
- Check status is PENDING (not already sent/cancelled)
- Verify user preferences - channel might be disabled
- Check ProcessPendingNotifications is being called
- Review notification_service logs

**Issue: Emails stuck in FAILED**
- Check email_logs.error field for SMTP error
- Verify SMTP configuration
- Check max_retries - may have exceeded attempts
- Check if scheduled_at is in future

**Issue: Template variables not rendering**
- Verify variable names match exactly {{.VarName}}
- Check TemplateVariable.Required = true for missing vars
- Ensure Data map includes all required variables
- Review template_service RenderTemplate method

**Issue: RabbitMQ messages not published**
- Check outbox_messages table for PENDING/FAILED status
- Verify RabbitMQ connection: rabbitmq.HealthCheck()
- Check outbox_processing_logs for errors
- Review rabbitmq_service processOutboxMessages

**Issue: Memory leak or goroutine bloat**
- ProcessPendingNotifications spawns goroutines
- Ensure goroutines complete (long running queries?)
- Monitor goroutine count with pprof
- Add timeouts to context in Send operations

---

## Related Files in Codebase

- User authentication: `/internal/modules/identity/`
- Email service base: `/internal/infrastructure/email/`
- Event system: `/internal/infrastructure/messaging/`
- Config: `/internal/core/config/`
- Logger: `/internal/core/logger/`
- Errors: `/internal/core/errors/`

