# Notification System Documentation Index

## Quick Navigation

This codebase includes comprehensive documentation for the notification system in three documents:

### 1. NOTIFICATION_SYSTEM_ANALYSIS.md (35 KB, 1,097 lines)
**Comprehensive Technical Overview** - Read this first for a complete understanding

Contents:
- Architecture overview with diagram
- 1. Notification triggering and creation (entry points, flow)
- 2. Notification types and channels (email, SMS, push, in-app, webhooks)
- 3. Template storage and usage (domain models, service operations, rendering)
- 4. Notification delivery mechanisms (pipeline, email specifics, preferences)
- 5. Events, notifications, and message queue relationship
- 6. Notification preference system (domain model, management, repository)
- 7. Background workers and jobs (scheduled processing, current gaps)
- 8. API endpoints (template handler routes)
- Complete data flow example (user registration → verification email)
- Database schema (all tables with fields)
- Key design patterns (repository, service, DDD, outbox, event-driven, DI)
- Error handling and resilience
- Security considerations
- Performance optimizations
- Summary table
- Integration points

**Best for:** Understanding the full system architecture and how components interact

---

### 2. NOTIFICATION_QUICK_REFERENCE.md (9.3 KB, 330 lines)
**Developer Quick Start Guide** - Use this for day-to-day development

Contents:
- Core files location map (organized by layer)
- Quick code examples (8 common operations)
- Notification status flow diagram
- Template variable syntax reference
- SQL queries for common operations
- Environment configuration template
- Key metrics to monitor
- Troubleshooting guide for common issues
- Related files in codebase

**Best for:** Quick lookup while coding, common patterns, troubleshooting

---

### 3. NOTIFICATION_DIAGRAMS.md (33 KB, 716 lines)
**Visual Flow Diagrams** - Reference these for understanding processes

Contents:
- 1. Complete notification flow (trigger → creation → delivery)
- 2. Template rendering pipeline (step-by-step)
- 3. Database schema relationships (entity diagrams)
- 4. Service architecture layers (layered architecture)
- 5. Priority and retry logic flow (state machine)
- 6. Environment and configuration flow (startup sequence)

**Best for:** Visual learners, understanding process flows, architecture overview

---

## Where to Start Based on Your Needs

### "I need to understand the entire system"
1. Start: NOTIFICATION_DIAGRAMS.md section 1 (Complete Notification Flow)
2. Then: NOTIFICATION_SYSTEM_ANALYSIS.md sections 1-5
3. Deep dive: Remaining sections as needed

### "I need to send a notification"
1. Quick start: NOTIFICATION_QUICK_REFERENCE.md section 1 (Send Email Notification)
2. Reference: NOTIFICATION_SYSTEM_ANALYSIS.md section 4 (Delivery Mechanisms)
3. See diagrams: NOTIFICATION_DIAGRAMS.md section 1

### "I need to create/manage templates"
1. Quick start: NOTIFICATION_QUICK_REFERENCE.md section 3 (Create Custom Template)
2. Reference: NOTIFICATION_SYSTEM_ANALYSIS.md section 3 (Templates)
3. See diagrams: NOTIFICATION_DIAGRAMS.md section 2 (Template Rendering Pipeline)

### "I need to debug a problem"
1. Check: NOTIFICATION_QUICK_REFERENCE.md Troubleshooting section
2. Understand flow: NOTIFICATION_DIAGRAMS.md relevant section
3. Deep dive: NOTIFICATION_SYSTEM_ANALYSIS.md error handling section

### "I need to integrate with another system"
1. Reference: NOTIFICATION_SYSTEM_ANALYSIS.md section 5 (Events & RabbitMQ)
2. Understand outbox: NOTIFICATION_SYSTEM_ANALYSIS.md section 5 (Transactional Outbox)
3. See diagrams: NOTIFICATION_DIAGRAMS.md section 6 (Configuration Flow)

### "I need to add a new notification channel"
1. Check current channels: NOTIFICATION_SYSTEM_ANALYSIS.md section 2
2. See service structure: NOTIFICATION_SYSTEM_ANALYSIS.md section 4
3. Review code: `/internal/modules/notification/service/notification_service.go`
4. See flow: NOTIFICATION_DIAGRAMS.md section 1

---

## Key Concepts Quick Reference

### Notification Types
- **Email**: Full implementation via SMTP (gomail)
- **SMS**: Placeholder, ready for Twilio/AWS SNS
- **Push**: Placeholder, ready for FCM/APNs
- **In-App**: Stored in DB, ready for WebSocket delivery
- **Webhook**: Placeholder with payload structure

### Status Lifecycle
```
PENDING → PROCESSING → SENT
  ↓         ↓
FAILED (with retry logic)
```

### Key Tables
- `notifications` - Main notification records
- `notification_templates` - Template definitions
- `template_languages` - Multi-language support
- `template_variables` - Variable definitions
- `notification_preferences` - User settings
- `email_logs` - Email delivery audit trail
- `outbox_messages` - Transactional outbox for events
- `outbox_processing_logs` - Message publishing history

### Core Services
- `NotificationService` - Send/retry notifications
- `TemplateService` - Template management & rendering
- `EnhancedEmailService` - Database-backed emails
- `EventDispatcher` - Publish domain events
- `RabbitMQService` - Message queue integration

### Design Patterns Used
- **Repository Pattern** - Abstract database operations
- **Service Layer** - Business logic orchestration
- **Domain-Driven Design** - Rich domain models
- **Transactional Outbox** - Guaranteed event delivery
- **Event-Driven Architecture** - Async communication
- **Dependency Injection** - Loose coupling

---

## File Organization

```
/internal/modules/notification/
├── domain/                      (Business objects)
│   ├── notification.go         (Notification, EmailLog, Preference)
│   └── template.go             (Template variants, variables)
├── service/                    (Business logic)
│   ├── notification_service.go (Main orchestration)
│   ├── template_service.go     (Template management)
│   └── enhanced_email_service.go (Database templates)
├── repository/                 (Data access)
│   ├── notification_repository.go (Interface)
│   ├── notification_repository_impl.go (GORM impl)
│   └── template_repository.go  (Template CRUD)
└── api/
    └── template_handler.go     (HTTP endpoints)

/internal/infrastructure/
├── email/                      (SMTP)
│   └── email_service.go        (gomail wrapper)
└── messaging/
    ├── events/                 (Event dispatcher)
    │   └── event_dispatcher.go
    ├── rabbitmq/              (Message queue)
    │   └── rabbitmq_service.go
    └── domain/                (Event models)
        └── outbox.go
```

---

## Configuration Required

### Environment Variables
```
EMAIL_SMTP_HOST=smtp.example.com
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USER=noreply@example.com
EMAIL_SMTP_PASSWORD=xxxxx
EMAIL_FROM_NAME=MyApp
EMAIL_FROM_EMAIL=noreply@example.com

RABBITMQ_URL=amqp://guest:guest@localhost:5672/
RABBITMQ_EXCHANGE=go-core

DATABASE_URL=postgres://user:pass@host:5432/db

APP_BASE_URL=https://app.example.com
APP_NAME=MyApplication
```

---

## Performance Considerations

1. **Goroutines**: Immediate notifications spawn goroutines (monitor count)
2. **Database**: Index on user_id, type, status, scheduled_at
3. **Outbox Processing**: Runs every 5 seconds (configurable)
4. **Template Rendering**: In-memory templates recommended for production
5. **Email**: Batch operations possible but not implemented
6. **RabbitMQ**: Connection pooling, reconnection logic included

---

## Testing Integration Points

| Operation | Test File | Method |
|-----------|-----------|--------|
| Send Email | `notification_service_test.go` | `TestSendEmail` |
| Render Template | `template_service_test.go` | `TestRenderTemplate` |
| Process Pending | `notification_service_test.go` | `TestProcessPending` |
| User Preferences | `notification_service_test.go` | `TestGetPreferences` |
| Template CRUD | `template_service_test.go` | `TestCreate/Update/Delete` |

---

## Common Questions & Answers

**Q: How long are notifications retained?**
A: Indefinitely unless soft-deleted. Consider archiving old records.

**Q: Can I send notifications without templates?**
A: Yes, use `SendNotification()` with inline content.

**Q: What's the max email recipients per notification?**
A: Limited only by JSON field size (PostgreSQL JSONB ~1GB).

**Q: How are scheduled notifications handled?**
A: ProcessPendingNotifications() checks scheduled_at timestamp.

**Q: Is there a queue size limit?**
A: RabbitMQ queue limits dependent on broker config.

**Q: Can I customize template variables?**
A: Yes, TemplateVariable model supports custom types.

**Q: How do I track email opens/clicks?**
A: EmailLog has opened_at, clicked_at fields (not auto-populated).

**Q: Can I have multi-language notifications?**
A: Yes, TemplateLanguage model handles variants per language.

---

## Next Steps

1. **Set up environment**: Copy configuration template from NOTIFICATION_QUICK_REFERENCE.md
2. **Initialize database**: Run migrations for notification tables
3. **Create system templates**: Call `InitializeSystemTemplates()`
4. **Start background jobs**: Schedule ProcessPendingNotifications & RetryFailedNotifications
5. **Integrate with auth**: Hook notification sending to registration/password reset events
6. **Configure SMTP**: Test with MailHog or real SMTP server
7. **Configure RabbitMQ**: Set up broker and queues
8. **Monitor metrics**: Track success rates, latency, retry counts

---

## Support & Documentation

For detailed information on any topic:
1. Search this index first
2. Check relevant documentation file
3. Review source code in `/internal/modules/notification/`
4. Check tests for usage examples
5. Review example flows in NOTIFICATION_DIAGRAMS.md

**Generated**: December 2025
**System Version**: Production-ready
**Last Updated**: 2025-12-13
