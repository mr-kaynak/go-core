# Notification System - Visual Diagrams

## 1. Complete Notification Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    1. TRIGGER / CREATION                            │
└─────────────────────────────────────────────────────────────────────┘

API Call: POST /notifications/send
    │
    ├─→ SendEmail() or SendNotification()
    │   ├─→ Check user preferences (enabled?)
    │   ├─→ Create Notification record (PENDING)
    │   ├─→ Store recipients as JSON
    │   ├─→ Store metadata with template data
    │   └─→ Save to database
    │
    ├─→ If scheduled (ScheduledAt > NOW)
    │   └─→ Return notification, wait for scheduled time
    │
    └─→ If immediate
        ├─→ Return notification immediately
        └─→ Spawn goroutine for processing (non-blocking)

┌─────────────────────────────────────────────────────────────────────┐
│              2. IMMEDIATE SENDING (Goroutine)                       │
└─────────────────────────────────────────────────────────────────────┘

processNotification(notification)
    │
    ├─→ Update status: PENDING → PROCESSING
    │
    ├─→ Route by type:
    │   │
    │   ├─→ EMAIL:
    │   │   ├─→ sendEmailNotification()
    │   │   ├─→ Parse recipients from JSON
    │   │   ├─→ Parse metadata (template vars)
    │   │   ├─→ Call emailService.Send()
    │   │   │   ├─→ Create gomail.Message
    │   │   │   ├─→ Set headers (From, To, Subject)
    │   │   │   ├─→ Set priority headers
    │   │   │   ├─→ Connect to SMTP
    │   │   │   └─→ Deliver message
    │   │   ├─→ Create EmailLog record
    │   │   └─→ Update notification status
    │   │
    │   ├─→ SMS:
    │   │   ├─→ sendSMSNotification()
    │   │   ├─→ Parse phone from metadata
    │   │   └─→ Call SMS provider (ready for integration)
    │   │
    │   ├─→ PUSH:
    │   │   ├─→ sendPushNotification()
    │   │   ├─→ Parse device_tokens from metadata
    │   │   └─→ Call FCM/APNs provider
    │   │
    │   ├─→ IN_APP:
    │   │   ├─→ sendInAppNotification()
    │   │   ├─→ Store in database (already done)
    │   │   └─→ Ready for WebSocket/polling delivery
    │   │
    │   └─→ WEBHOOK:
    │       ├─→ sendWebhookNotification()
    │       ├─→ Parse webhook_url from metadata
    │       └─→ POST to webhook endpoint
    │
    ├─→ Handle success/failure
    │   ├─→ Success: status = SENT, set sent_at timestamp
    │   └─→ Failure: status = FAILED, store error message
    │
    └─→ Update notification in database

┌─────────────────────────────────────────────────────────────────────┐
│         3. BACKGROUND PROCESSING (Cron Jobs)                        │
└─────────────────────────────────────────────────────────────────────┘

Every 5 seconds: ProcessPendingNotifications(limit)
    │
    ├─→ Query: status = PENDING AND (scheduled_at IS NULL OR scheduled_at <= NOW)
    ├─→ Order by: priority DESC, created_at ASC
    ├─→ Limit: 100 at a time
    │
    └─→ For each notification:
        └─→ Spawn goroutine → processNotification() [See section 2 above]

Every 1 minute: RetryFailedNotifications(limit)
    │
    ├─→ Query: status = FAILED AND retry_count < max_retries
    ├─→ Order by: priority DESC, created_at ASC
    ├─→ Limit: 50 at a time
    │
    └─→ For each notification:
        ├─→ Increment retry_count
        ├─→ Reset status to PENDING
        ├─→ Save to database
        └─→ Spawn goroutine → processNotification() [See section 2 above]

┌─────────────────────────────────────────────────────────────────────┐
│          4. EVENT DISPATCHER & OUTBOX PATTERN                       │
└─────────────────────────────────────────────────────────────────────┘

Application Event (e.g., user.registered)
    │
    ├─→ eventDispatcher.Dispatch(ctx, DomainEvent)
    │   ├─→ Execute local handlers (synchronously)
    │   │   └─→ May trigger notification sending
    │   │
    │   ├─→ Convert to RabbitMQ Message
    │   │
    │   └─→ rabbitmq.PublishMessage(ctx, message)
    │       ├─→ Serialize message to JSON
    │       ├─→ Create OutboxMessage record
    │       │   ├─→ Status: PENDING
    │       │   ├─→ Payload: JSON
    │       │   ├─→ RoutingKey: event.Type (e.g., "user.registered")
    │       │   └─→ Save to database (TRANSACTIONAL)
    │       │
    │       └─→ Return success immediately
    │
    └─→ Return to caller

Every 5 seconds: RabbitMQ.processOutboxMessages()
    │
    ├─→ Query: OutboxMessage where status = PENDING OR (status = FAILED AND canRetry)
    ├─→ Limit: 10 pending + 5 retry
    │
    └─→ For each message:
        ├─→ Update status: PENDING → PROCESSING
        ├─→ rabbitmq.PublishDirectly(ctx, routingKey, message)
        │   ├─→ Connect to RabbitMQ
        │   ├─→ Publish to exchange with routing key
        │   ├─→ Set DeliveryMode: Persistent
        │   └─→ Return
        │
        ├─→ If success:
        │   ├─→ Mark as SENT
        │   ├─→ Clear error field
        │   └─→ Log to OutboxProcessingLog
        │
        ├─→ If failure:
        │   ├─→ Mark as FAILED
        │   ├─→ If canRetry():
        │   │   ├─→ Increment retry_count
        │   │   ├─→ Set nextRetryAt (exponential backoff)
        │   │   └─→ Keep status as FAILED (not PENDING yet)
        │   │
        │   ├─→ Else (exceeded max retries):
        │   │   ├─→ Move to DLQ (status = DLQ)
        │   │   ├─→ Create OutboxDeadLetter record
        │   │   └─→ Log to OutboxProcessingLog
        │   │
        │   └─→ Log to OutboxProcessingLog with error
        │
        └─→ Save OutboxMessage updates

┌─────────────────────────────────────────────────────────────────────┐
│              5. RABBITMQ DELIVERY                                    │
└─────────────────────────────────────────────────────────────────────┘

RabbitMQ Message Broker
    │
    ├─→ Receives message from application
    ├─→ Routes by exchange (topic) + routing key
    ├─→ Delivers to subscribed queues
    ├─→ Acknowledgment handling
    │   ├─→ ACK: Message consumed, remove from queue
    │   ├─→ NACK with requeue: Message goes back to queue
    │   └─→ NACK without requeue: Message goes to DLQ
    │
    └─→ Consumer applications handle message
        └─→ Execute handlers for event

```

---

## 2. Template Rendering Pipeline

```
Client Request: RenderTemplate
    │
    ├─→ Input:
    │   ├─→ TemplateName: "welcome_user"
    │   ├─→ LanguageCode: "en"
    │   └─→ Data:
    │       ├─→ Username: "Alice"
    │       ├─→ AppName: "MyApp"
    │       └─→ LoginURL: "https://app.com"
    │
    ├─→ TemplateService.RenderTemplate()
    │
    ├─→ Step 1: Load Template
    │   ├─→ Query: notification_templates WHERE name = "welcome_user"
    │   ├─→ Join: template_languages (en, tr, es variants)
    │   ├─→ Join: template_variables (required vars list)
    │   └─→ Load from database
    │
    ├─→ Step 2: Validate Variables
    │   ├─→ Get required variables:
    │   │   ├─→ Username (required)
    │   │   ├─→ AppName (required)
    │   │   └─→ LoginURL (required)
    │   │
    │   └─→ Check data contains all required:
    │       ├─→ data["Username"] ✓
    │       ├─→ data["AppName"] ✓
    │       ├─→ data["LoginURL"] ✓
    │       └─→ Return error if missing
    │
    ├─→ Step 3: Select Language
    │   ├─→ Exact match: LanguageCode = "en" → Found ✓
    │   ├─→ If not found:
    │   │   ├─→ Try: isDefault = true
    │   │   ├─→ If not found:
    │   │   │   └─→ Return first available
    │   │   └─→ Return fallback variant
    │   │
    │   └─→ Get variant:
    │       ├─→ Subject: "Welcome to {{.AppName}}!"
    │       └─→ Body: "Hello {{.Username}},...\n{{.LoginURL}}"
    │
    ├─→ Step 4: Apply Defaults
    │   ├─→ For each template variable:
    │   │   ├─→ If not in data AND has defaultValue:
    │   │   │   └─→ data[varName] = defaultValue
    │   │   └─→ Else: keep as is (missing ok for non-required)
    │
    ├─→ Step 5: Render with Go Templates
    │   │
    │   ├─→ Parse subject template:
    │   │   "Welcome to {{.AppName}}!"
    │   │
    │   ├─→ Custom functions available:
    │   │   ├─→ upper, lower, title, trim, capitalize
    │   │   ├─→ pluralize, formatDate, default
    │   │   └─→ Built-in: if, range, with, etc.
    │   │
    │   ├─→ Execute with data:
    │   │   ├─→ {{.AppName}} → "MyApp"
    │   │   └─→ Result: "Welcome to MyApp!"
    │   │
    │   └─→ Parse body template:
    │       "Hello {{.Username}},\n...\nVisit {{.LoginURL}}"
    │
    │       Execute with data:
    │       ├─→ {{.Username}} → "Alice"
    │       ├─→ {{.LoginURL}} → "https://app.com"
    │       └─→ Result: "Hello Alice,\n...\nVisit https://app.com"
    │
    ├─→ Step 6: Increment Usage (async)
    │   ├─→ template.usage_count += 1
    │   ├─→ template.last_used_at = NOW()
    │   └─→ Save asynchronously
    │
    └─→ Output: RenderedTemplate
        ├─→ Subject: "Welcome to MyApp!"
        └─→ Body: "Hello Alice,\n...\nVisit https://app.com"
```

---

## 3. Database Schema Relationships

```
┌──────────────────────┐
│     Users Table      │
│  (External Module)   │
├──────────────────────┤
│ id (UUID) ◄──────┐   │
│ email            │   │
│ username         │   │
│ created_at       │   │
└──────────────────┘   │
        ▲              │
        │              │
        │ FK           │
        │              │
┌───────┴──────────────────────┐
│ notification_preferences     │
├──────────────────────────────┤
│ id (UUID)                    │
│ user_id (FK) ◄───┐           │
│ email_enabled    │ 1:1       │
│ sms_enabled      │ (unique)  │
│ push_enabled     │           │
│ in_app_enabled   │           │
│ email_frequency  │           │
│ quiet_hours_*    │           │
│ timezone         │           │
│ language         │           │
└──────────────────────────────┘
                    ▲
                    │
                    └───┐
                        │
┌─────────────────────────────────────┐
│        notifications                │
├─────────────────────────────────────┤
│ id (UUID)                           │
│ user_id (FK) ◄───┐ 1:M             │
│ type (email, sms, push, ...)        │
│ status (pending, sent, failed, ...)│
│ priority                            │
│ subject                             │
│ content                             │
│ template (name reference)           │
│ recipients (JSON)                   │
│ metadata (JSONB)                    │
│ scheduled_at                        │
│ sent_at                             │
│ failed_at                           │
│ error                               │
│ retry_count / max_retries           │
└──────────────────────┬──────────────┘
                       │ 1:1 (optional)
                       │ FK (notification_id)
                       │
        ┌──────────────┴────────────┐
        │                           │
        ▼                           ▼
┌─────────────────┐    ┌───────────────────────┐
│   email_logs    │    │ (Future: SMS, Push)   │
├─────────────────┤    └───────────────────────┘
│ id (UUID)       │
│ notif_id (FK)   │
│ from            │
│ to              │
│ cc / bcc        │
│ subject         │
│ body            │
│ status          │
│ smtp_response   │
│ message_id      │
│ error           │
│ opened_at       │
│ clicked_at      │
│ bounced_at      │
└─────────────────┘

┌──────────────────────────────────────┐
│  notification_templates              │
├──────────────────────────────────────┤
│ id (UUID)                            │
│ name (unique)                        │
│ type (email, sms, push, ...)        │
│ subject                              │
│ body                                 │
│ is_active                            │
│ is_system                            │
│ category_id (FK) ◄────────┐          │
│ version                    │ 1:M      │
│ usage_count                │          │
│ last_used_at               │          │
└──────────────────────────┬─────────────┘
        │                  │
        │ 1:M              │
        ├──────────────────┘
        │
        ├─→ template_languages
        │   ├─→ template_id (FK)
        │   ├─→ language_code
        │   ├─→ subject
        │   ├─→ body
        │   └─→ is_default
        │
        └─→ template_variables
            ├─→ template_id (FK)
            ├─→ name
            ├─→ type
            ├─→ required
            └─→ default_value

┌────────────────────────┐
│ template_categories    │
├────────────────────────┤
│ id (UUID)              │
│ name (unique)          │
│ description            │
│ parent_id (FK) ◄─┐     │ (Self-join for hierarchy)
│                 └─────►│
└────────────────────────┘

┌──────────────────────────┐
│   outbox_messages        │
├──────────────────────────┤
│ id (UUID)                │
│ aggregate_id (UUID)      │ (FK to any aggregate: notification, user, etc.)
│ aggregate_type (str)     │ (e.g., "Notification", "User")
│ event_type (str)         │ (e.g., "notification.sent", "user.registered")
│ payload (JSONB)          │ (Full message)
│ status (pending/sent/..) │
│ queue (exchange)         │
│ routing_key              │
│ priority                 │
│ retry_count / max_retries│
│ next_retry_at            │
│ processed_at             │
│ failed_at                │
│ error                    │
│ correlation_id           │ (For distributed tracing)
│ causation_id             │ (What caused this event)
│ ttl                      │
└──────────────────────────┘
        │ 1:M
        │
        ├─→ outbox_processing_logs
        │   ├─→ action (sent, failed, retried, moved_to_dlq)
        │   ├─→ status
        │   ├─→ error
        │   └─→ processing_time_ms
        │
        └─→ outbox_dead_letters (moved when max retries exceeded)
            ├─→ original_message (JSONB copy)
            ├─→ failure_reason
            ├─→ retry_count
            ├─→ last_error
            └─→ reprocessed (boolean for manual handling)
```

---

## 4. Service Architecture Layers

```
┌─────────────────────────────────────────────┐
│          HTTP API LAYER (Fiber)             │
│                                             │
│ POST /notifications/send                    │
│ POST /notifications/email                   │
│ GET  /notifications/:id                     │
│ GET  /notifications/user/:userId            │
│ GET  /notifications/:userId/preferences    │
│ PUT  /notifications/:userId/preferences    │
│                                             │
│ POST /templates                             │
│ GET  /templates                             │
│ POST /templates/render                      │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────┐
│    HANDLER LAYER (Controllers)      │
│                                     │
│ TemplateHandler                     │
│ NotificationHandler (implied)       │
└──────────────┬──────────────────────┘
               │
┌──────────────▼────────────────────────────┐
│      SERVICE LAYER (Business Logic)       │
│                                           │
│ NotificationService                       │
│ ├─→ SendEmail()                           │
│ ├─→ SendNotification()                    │
│ ├─→ ProcessPendingNotifications()         │
│ ├─→ RetryFailedNotifications()            │
│ └─→ Get/Update Preferences                │
│                                           │
│ TemplateService                           │
│ ├─→ CreateTemplate()                      │
│ ├─→ GetTemplate() / GetTemplateByName()   │
│ ├─→ UpdateTemplate()                      │
│ ├─→ DeleteTemplate()                      │
│ ├─→ RenderTemplate()                      │
│ └─→ Category management                   │
│                                           │
│ EnhancedEmailService                      │
│ ├─→ SendWithTemplate()                    │
│ ├─→ SendVerificationEmail()               │
│ ├─→ SendPasswordResetEmail()              │
│ ├─→ SendWelcomeEmail()                    │
│ └─→ SendBulkEmails()                      │
│                                           │
│ EmailService (Base)                       │
│ └─→ Send() [SMTP via gomail]              │
│                                           │
│ EventDispatcher                           │
│ ├─→ Dispatch()                            │
│ ├─→ DispatchNotificationSent()            │
│ ├─→ DispatchUserRegistered() (etc)        │
│ └─→ Register local handlers               │
│                                           │
│ RabbitMQService                           │
│ ├─→ PublishMessage() [outbox]             │
│ ├─→ PublishDirectly()                     │
│ ├─→ Subscribe()                           │
│ ├─→ DeclareQueue()                        │
│ └─→ processOutboxMessages() [background]  │
└──────────────┬────────────────────────────┘
               │
┌──────────────▼────────────────────────────┐
│     REPOSITORY LAYER (Data Access)        │
│                                           │
│ NotificationRepository (interface)        │
│ ├─→ notificationRepositoryImpl             │
│ │   ├─→ CreateNotification()              │
│ │   ├─→ UpdateNotification()              │
│ │   ├─→ GetNotification()                 │
│ │   ├─→ GetUserNotifications()            │
│ │   ├─→ GetPendingNotifications()         │
│ │   ├─→ GetFailedNotifications()          │
│ │   └─→ CreateEmailLog()                  │
│ │                                         │
│ TemplateRepository (interface)            │
│ └─→ templateRepositoryImpl                 │
│     ├─→ CreateTemplate()                  │
│     ├─→ GetTemplateByID/Name()            │
│     ├─→ UpdateTemplate()                  │
│     ├─→ ListTemplates()                   │
│     ├─→ Language variant ops              │
│     ├─→ Variable ops                      │
│     └─→ Category ops                      │
│                                           │
│ OutboxRepository (for messaging)          │
│ └─→ CRUD for outbox_messages              │
└──────────────┬────────────────────────────┘
               │
┌──────────────▼────────────────────────────┐
│  DOMAIN LAYER (Business Objects/Models)   │
│                                           │
│ notification.go:                          │
│ ├─→ Notification                          │
│ ├─→ NotificationTemplate                  │
│ ├─→ NotificationPreference                │
│ └─→ EmailLog                              │
│                                           │
│ template.go:                              │
│ ├─→ ExtendedNotificationTemplate          │
│ ├─→ TemplateLanguage                      │
│ ├─→ TemplateVariable                      │
│ └─→ TemplateCategory                      │
│                                           │
│ Events (messaging):                       │
│ ├─→ DomainEvent                           │
│ ├─→ EventType constants                   │
│ └─→ OutboxMessage                         │
└──────────────┬────────────────────────────┘
               │
┌──────────────▼────────────────────────────┐
│    INFRASTRUCTURE LAYER (External I/O)    │
│                                           │
│ Database (PostgreSQL)                     │
│ ├─→ GORM ORM for queries                  │
│ ├─→ Connection pooling                    │
│ └─→ JSONB support                         │
│                                           │
│ SMTP (gomail)                             │
│ ├─→ TLS connections                       │
│ ├─→ Message composition                   │
│ └─→ Send/receive operations               │
│                                           │
│ RabbitMQ                                  │
│ ├─→ AMQP connection                       │
│ ├─→ Topic exchange                        │
│ ├─→ Queue binding                         │
│ └─→ Publisher/subscriber pattern          │
│                                           │
│ Configuration                             │
│ ├─→ SMTP settings                         │
│ ├─→ RabbitMQ URL                          │
│ ├─→ Database connection                   │
│ └─→ Feature flags                         │
│                                           │
│ Logging                                   │
│ └─→ Structured logging via custom logger  │
└────────────────────────────────────────────┘
```

---

## 5. Priority & Retry Logic Flow

```
Notification Created with Priority
    │
    ├─→ Priority: "urgent" (9) → Processed first
    ├─→ Priority: "high" (6) → Processed second
    ├─→ Priority: "normal" (3) → Standard queue
    └─→ Priority: "low" (1) → Processed last

Processing Order:
    ORDER BY priority DESC, created_at ASC

Status State Machine:
┌──────────┐
│ PENDING  │ ← Initial state after creation
└────┬─────┘
     │ ProcessNotification starts
     ▼
┌──────────────┐
│ PROCESSING   │ ← Update sent to DB immediately
└────┬─────────┘
     │ Try to send (SMTP/SMS/etc.)
     │
     ├─→ SUCCESS ────────────────────────────────┐
     │                                            │
     │   ├─→ Mark as SENT                        │
     │   ├─→ Set sent_at = NOW()                 │
     │   └─→ Create EmailLog (status: sent)      │
     │                                            │
     └─────────────────────────────────────────→ ┌─────────┐
                                                  │  SENT   │✓ (Terminal)
                                                  └─────────┘

     ├─→ FAILURE ────────────────────────────────┐
     │                                            │
     │   ├─→ Mark as FAILED                      │
     │   ├─→ Set failed_at = NOW()               │
     │   ├─→ Store error message                 │
     │   ├─→ Create EmailLog (status: failed)    │
     │   └─→ Check if can retry:                 │
     │       (retry_count < max_retries)         │
     │                                            │
     └──────────────┬───────────────────────────────────────────┐
                    │                                           │
         CAN RETRY  │                         CANNOT RETRY      │
         (count<3)  │                         (count>=3)        │
                    │                                           │
                    ▼                                           ▼
         ┌──────────────────┐                          ┌──────────────┐
         │      FAILED      │                          │     FAILED   │
         │  (retry_count=1) │ ← Wait for retry        │   (PERMANENT)│
         └────────┬─────────┘    (exponential backoff) └──────────────┘
                  │
       1 sec wait, then:
       RetryFailedNotifications()
       ├─→ Set status back to PENDING
       ├─→ Spawn new goroutine
       └─→ Go back to PROCESSING state

Can also manually mark as:
  ├─→ CANCELLED (user action, not retried)
  └─→ BOUNCED (email provider feedback)
```

---

## 6. Environment & Configuration Flow

```
┌─────────────────────────────────────────┐
│      Application Startup                │
└────────────────┬────────────────────────┘
                 │
         ┌───────▼────────┐
         │ Load .env file │
         └───────┬────────┘
                 │
    ┌────────────┴───────────────┬──────────────────┐
    │                            │                  │
    ▼                            ▼                  ▼
EMAIL_*              RABBITMQ_*           DATABASE_*
├─→ SMTP_HOST        ├─→ URL               ├─→ URL
├─→ SMTP_PORT        ├─→ EXCHANGE          ├─→ USER
├─→ SMTP_USER        └─→ (host/port/creds) ├─→ PASS
├─→ SMTP_PASSWORD                         └─→ DBNAME
├─→ FROM_NAME
├─→ FROM_EMAIL       
└─→ (TLS settings)

        │
        ▼
    config.Config loaded
        │
        ├─→ Create EmailService
        │   └─→ Initialize SMTP dialer
        │       └─→ Test connection
        │
        ├─→ Create RabbitMQService
        │   └─→ Connect to broker
        │       ├─→ Declare exchange
        │       ├─→ Start outbox processor
        │       └─→ Start reconnection monitor
        │
        ├─→ Create NotificationRepository
        │   └─→ Use database connection (GORM)
        │
        ├─→ Create TemplateService
        │   └─→ Inject repository
        │
        ├─→ Create EnhancedEmailService
        │   └─→ Inject template service
        │
        ├─→ Create NotificationService
        │   ├─→ Inject repository
        │   └─→ Inject email service
        │
        ├─→ Create EventDispatcher
        │   └─→ Inject RabbitMQ service
        │
        └─→ Register route handlers
            ├─→ Template routes
            └─→ Notification routes (implied)

        │
        ▼
    Initialize system templates
    └─→ 5 default templates created in DB
        ├─→ user_verification
        ├─→ password_reset
        ├─→ welcome_user
        ├─→ account_locked
        └─→ two_factor_code

        │
        ▼
    Start background schedulers
    ├─→ cron.AddFunc("@every 5s", ProcessPendingNotifications)
    └─→ cron.AddFunc("@every 1m", RetryFailedNotifications)

        │
        ▼
    Application Ready
    (Listen for HTTP requests + process background jobs)
```

