# Comprehensive Notification System Analysis

## Overview
This Go codebase implements a sophisticated, production-ready notification system with support for multiple delivery channels, template management, event-driven architecture, and reliable message delivery using the transactional outbox pattern with RabbitMQ.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         APPLICATION LAYER                              │
│  (API Handlers, Services, Events)                                      │
└──────────────────────┬──────────────────────────────────────────────────┘
                       │
         ┌─────────────┴─────────────┬────────────────────┐
         │                           │                    │
┌────────▼────────┐    ┌─────────────▼──────┐  ┌──────────▼──────────┐
│  Notification   │    │  Template Service  │  │ Event Dispatcher    │
│  Service        │    │                    │  │                     │
│  - SendEmail    │    │ - Render Templates │  │ - Dispatch Events   │
│  - SendSMS      │    │ - Store in DB      │  │ - Publish to MQ     │
│  - SendPush     │    │ - Language Support │  │ - Local Handlers    │
│  - SendWebhook  │    │ - Variable Mgmt    │  │                     │
│  - SendInApp    │    └────────────────────┘  └──────────────────────┘
└────────┬────────┘
         │
         ├──────────────────┬──────────────────┐
         │                  │                  │
    ┌────▼─────┐    ┌───────▼────────┐    ┌───▼─────────┐
    │  Email   │    │  Notification  │    │  Database   │
    │ Service  │    │  Preferences   │    │  (GORM)     │
    │(gomail)  │    │  Repository    │    │             │
    └────┬─────┘    └────────────────┘    │ Tables:     │
         │                                 │ - Notifs    │
         │                                 │ - Templates │
         └─────────────┬──────────────────►│ - Prefs     │
                       │                   │ - EmailLogs │
                  ┌────▼────────────────┐  └─────────────┘
                  │  Transactional      │
                  │  Outbox Pattern     │
                  │  - OutboxMessage    │
                  │  - DLQ Support      │
                  │  - Processing Logs  │
                  └────┬────────────────┘
                       │
                  ┌────▼──────────────────┐
                  │   RabbitMQ Service    │
                  │                       │
                  │ - Connection Mgmt     │
                  │ - Queue Declaration   │
                  │ - Message Publishing  │
                  │ - Outbox Processing   │
                  │ - Reconnection Logic  │
                  └────┬──────────────────┘
                       │
                       ▼
                  ┌──────────────┐
                  │  RabbitMQ    │
                  │  Message     │
                  │  Broker      │
                  └──────────────┘
```

---

## 1. NOTIFICATION TRIGGERING/CREATION

### Entry Points

#### A. Synchronous Notification Creation
**File**: `/internal/modules/notification/service/notification_service.go`

Two main methods for creating notifications:

```go
SendEmail(req *SendEmailRequest) (*domain.Notification, error)
SendNotification(req *SendNotificationRequest) (*domain.Notification, error)
```

**SendEmailRequest Structure:**
- UserID (required)
- To, CC, BCC email addresses
- Subject (required)
- Template name (required)
- Template data (optional)
- Priority (low, normal, high, urgent)
- ScheduledAt (optional, for delayed sending)

**SendNotificationRequest Structure:**
- UserID (required)
- Type (email, sms, push, in_app, webhook)
- Priority
- Subject & Content
- Template name
- Recipients list
- Metadata (custom data)
- ScheduledAt

#### B. Process Flow

1. **Notification Creation**
   - Validate user notification preferences (check if channel is enabled)
   - Create notification record in database with `PENDING` status
   - Store metadata as JSON
   - Return notification object

2. **Immediate vs. Scheduled**
   - If `ScheduledAt` is in future: only save, return notification
   - If immediate or `ScheduledAt` is null: spawn goroutine to process immediately
   - Scheduled notifications processed by `ProcessPendingNotifications()` background task

#### C. Asynchronous Processing Entry Points

```go
ProcessPendingNotifications(limit int) error
RetryFailedNotifications(limit int) error
GetScheduledNotifications(limit int) error
```

These are meant to be called by background jobs/cron tasks (currently called from services but typically invoked by a scheduler)

---

## 2. NOTIFICATION TYPES & CHANNELS SUPPORTED

### Type Constants
**File**: `/internal/modules/notification/domain/notification.go`

```go
NotificationTypeEmail   = "email"
NotificationTypeSMS     = "sms"
NotificationTypePush    = "push"
NotificationTypeInApp   = "in_app"
NotificationTypeWebhook = "webhook"
```

### Channel-Specific Implementation

#### Email
- **Service**: `EnhancedEmailService` + base `EmailService`
- **SMTP Configuration**: Uses gomail library with TLS support
- **Features**:
  - Template rendering with variable substitution
  - Priority headers (X-Priority, Importance)
  - CC/BCC support
  - Attachments
  - Language-specific variants
  - Tracking headers (X-Template, X-Language)
  - Email logs for audit trail

#### SMS
- **Current**: Placeholder implementation (logs intent)
- **Design**: Expects phone number in notification metadata
- **Integration Point**: Ready for Twilio, AWS SNS, or similar provider

#### Push Notifications
- **Current**: Placeholder implementation (logs intent)
- **Design**: Expects device_tokens array in metadata
- **Integration Point**: Ready for FCM, APNs integration

#### In-App Notifications
- **Current**: Stores in database for client polling/WebSocket
- **Design**: Ready for WebSocket/SSE real-time delivery
- **Storage**: Notification record itself serves as message store
- **Delivery**: Clients poll via API or subscribe via WebSocket

#### Webhooks
- **Current**: Placeholder with payload construction
- **Design**: Expects webhook_url in metadata
- **Features**: Constructs structured JSON payload with all notification details
- **Integration Point**: Ready for retry logic and webhook signature support

### Notification Status Lifecycle

```
PENDING → PROCESSING → SENT
   ↓          ↓
 FAILED ← (retry logic) → PROCESSING → SENT
   ↓
CANCELLED / BOUNCED
```

---

## 3. TEMPLATE STORAGE & USAGE

### Template Domain Models
**File**: `/internal/modules/notification/domain/template.go`

#### Core Entities

**NotificationTemplate** (Base)
- ID (UUID, primary key)
- Name (unique, required)
- Type (email, sms, push, in_app, webhook)
- Subject (optional)
- Body (required, text with Go template syntax)
- Variables (stored as JSON array)
- IsActive (default true)
- Description

**ExtendedNotificationTemplate** (With Relationships)
- Extends NotificationTemplate
- CategoryID (foreign key for organization)
- Category (relationship)
- Languages (array of TemplateLanguage)
- TemplateVariables (array of TemplateVariable)
- Tags (JSON array for classification)
- Version (for migrations)
- IsSystem (cannot be deleted)
- UsageCount & LastUsedAt (analytics)

**TemplateLanguage**
- TemplateID
- LanguageCode (e.g., "en", "tr", "es")
- Subject & Body (language-specific)
- IsDefault (selects default variant)

**TemplateVariable**
- TemplateID
- Name (e.g., "Username", "VerificationURL")
- Type (string, number, boolean, date)
- Required (boolean)
- DefaultValue (fallback if not provided)
- Description (for documentation)

**TemplateCategory**
- Name (unique)
- Description
- ParentID (hierarchical organization)
- Relationships to templates

### Template Service Operations
**File**: `/internal/modules/notification/service/template_service.go`

**CRUD Operations:**
```go
CreateTemplate(req *CreateTemplateRequest) 
GetTemplate(id uuid.UUID)
GetTemplateByName(name string)
UpdateTemplate(id, req)
DeleteTemplate(id) // Protected: cannot delete system templates
```

**Template Rendering:**
```go
RenderTemplate(req *RenderTemplateRequest) (*RenderedTemplate, error)
```

Steps:
1. Fetch template by name
2. Validate all required variables provided
3. Get language-specific variant (fallback to default/first)
4. Apply default values for missing optional variables
5. Render using Go html/template with custom functions
6. Increment usage count asynchronously

**Custom Template Functions Available:**
- `upper` - Convert to uppercase
- `lower` - Convert to lowercase
- `title` - Title case
- `trim` - Remove whitespace
- `capitalize` - First letter uppercase
- `pluralize` - Handle singular/plural
- `formatDate` - Date formatting
- `default` - Provide default value

**System Templates:**
Five predefined system templates auto-created:
1. `user_verification` - Email verification for new users
2. `password_reset` - Password reset requests
3. `welcome_user` - Welcome email after signup
4. `account_locked` - Account security alerts
5. `two_factor_code` - 2FA authentication codes

### Template Storage in Database

**Tables:**
- `notification_templates` - Base template data
- `template_languages` - Language variants
- `template_variables` - Variable definitions
- `template_categories` - Template organization

**Key Features:**
- Soft deletes (system templates protected)
- Usage tracking for analytics
- Version tracking for migrations
- JSONB fields for flexible metadata
- Unique constraints on name + language combo

---

## 4. NOTIFICATION DELIVERY MECHANISMS

### Delivery Pipeline

```
Notification Created
       ↓
Check User Preferences
       ↓
Save to Database
       ↓
If Immediate: Spawn Goroutine
       ↓
processNotification()
       ↓
Update Status: PENDING → PROCESSING
       ↓
Route by Type
       ├─→ Email: sendEmailNotification()
       ├─→ SMS: sendSMSNotification()
       ├─→ Push: sendPushNotification()
       ├─→ InApp: sendInAppNotification()
       └─→ Webhook: sendWebhookNotification()
       ↓
Update Status: PROCESSING → SENT or FAILED
       ↓
Save Email Log (for emails)
```

### Email Delivery Specifics

**Flow in NotificationService:**
1. Parse recipients from notification.Recipients (stored as JSON)
2. Parse metadata for template data
3. Prepare EmailData struct
4. Call emailSvc.Send(ctx, emailData)
5. Create EmailLog record
6. Update notification status

**EmailLog Tracking:**
- Stores: from, to, cc, bcc, subject, body
- Status: pending, sent, failed
- Error messages if failure
- SMTP response
- Open/click/bounce tracking fields

### Email Service (Enhanced)

**EnhancedEmailService** (`enhanced_email_service.go`):

**Methods:**
```go
SendWithTemplate(req *EmailRequest) error
SendVerificationEmail(to, username, token, languageCode)
SendPasswordResetEmail(to, username, token, languageCode)
SendWelcomeEmail(to, username, languageCode)
SendTwoFactorCode(to, username, code, languageCode)
SendAccountLockedNotification(to, username, reason, action, languageCode)
SendBulkEmails(recipients[], templateName, baseData, languageCode)
```

**Key Features:**
- Renders template from database
- Supports language-specific content
- SMTP connection testing
- TLS configuration (skipped for ports 25, 1025)
- Custom SMTP headers for tracking
- Gomail library for reliable sending

### Notification Preferences

**NotificationPreference Domain Model:**
```go
UserID uuid.UUID
EmailEnabled bool (default: true)
SMSEnabled bool (default: false)
PushEnabled bool (default: false)
InAppEnabled bool (default: true)
EmailFrequency string (immediate, daily, weekly)
UnsubscribedTopics []string (JSON)
QuietHoursStart *time.Time (optional)
QuietHoursEnd *time.Time (optional)
Timezone string (default: UTC)
Language string (default: en)
```

**Current Check:**
- Service checks if channel enabled before sending
- Creates default preferences if not exist
- Prevents sending if user disabled that channel

---

## 5. RELATIONSHIP: NOTIFICATIONS, EVENTS, MESSAGE QUEUE

### Event-Driven Architecture

**File**: `/internal/infrastructure/messaging/events/event_dispatcher.go`

### Event Types Related to Notifications

```go
EventNotificationSent      = "notification.sent"
EventNotificationFailed    = "notification.failed"
EventNotificationScheduled = "notification.scheduled"
EventEmailSent            = "email.sent"
EventEmailBounced         = "email.bounced"
EventEmailOpened          = "email.opened"
EventEmailClicked         = "email.clicked"
EventTemplateCreated      = "template.created"
EventTemplateUpdated      = "template.updated"
EventTemplateDeleted      = "template.deleted"
EventTemplateUsed         = "template.used"
```

### DomainEvent Structure

```go
type DomainEvent struct {
    ID            string                 // UUID
    Type          EventType              // notification.sent, etc.
    AggregateID   string                 // Notification ID
    AggregateType string                 // "Notification"
    Timestamp     time.Time
    UserID        string
    TenantID      string
    CorrelationID string                 // For tracing
    CausationID   string                 // What caused this event
    Data          map[string]interface{} // Event payload
    Metadata      map[string]interface{}
    Version       int
}
```

### Event Dispatcher Workflow

```
Application Code
       ↓
eventDispatcher.DispatchNotificationSent(ctx, notificationID, userID, type)
       ↓
Convert to DomainEvent
       ↓
Execute Local Handlers (sync) ← Can listen to events within app
       ↓
Convert to RabbitMQ Message
       ↓
rabbitmq.PublishMessage(ctx, message) ← Outbox pattern
       ↓
Save to OutboxMessage table (transactional)
       ↓
RabbitMQ Service processes outbox asynchronously
       ↓
Publish to RabbitMQ (durable)
       ↓
Mark as sent in database
```

### Message Queue Integration (RabbitMQ)

**RabbitMQService** (`rabbitmq_service.go`):

**Key Features:**
1. **Topic Exchange**: Uses "topic" exchange type for routing
2. **Queue Declaration**: Creates queues with DLQ (Dead Letter Queue) config
3. **Message TTL**: 5 minutes (300000ms) per queue
4. **Max Retries**: 3 attempts before DLQ
5. **Persistent Delivery**: DeliveryMode set to Persistent

**Methods:**
```go
PublishMessage(ctx, message) // Via outbox (transactional)
PublishDirectly(ctx, routingKey, message) // Direct publish (bypasses outbox)
Subscribe(queueName, handler) // Subscribe to queue
DeclareQueue(name, routingKeys) // Create queue with config
```

**Reconnection Logic:**
- Monitors connection health
- Exponential backoff on reconnection (1s → max 30s)
- Automatic reconnection attempt
- Error channel monitoring

### Transactional Outbox Pattern

**File**: `/internal/infrastructure/messaging/domain/outbox.go`

**Purpose**: Guarantee exactly-once delivery even if application crashes

**Flow:**
1. Notification saved to DB (transaction 1)
2. OutboxMessage saved to same transaction
3. Either both succeed or both rollback
4. Separate background process reads outbox periodically
5. Publishes to RabbitMQ
6. On success: marks OutboxMessage as SENT
7. On failure: increments retry count or moves to DLQ

**OutboxMessage States:**
- PENDING: Ready to publish
- PROCESSING: Currently being published
- SENT: Successfully published
- FAILED: Publish failed
- DLQ: Permanently failed after max retries

**Dead Letter Queue (DLQ):**
- `outbox_messages` table with DLQ status
- `outbox_dead_letters` table for permanent failures
- Stores original message and failure reason
- Manual reprocessing possible

**Outbox Processing:**
- Runs every 5 seconds (configurable ticker)
- Processes up to 10 pending + 5 retry messages per batch
- Exponential backoff for retries (1s, 2s, 4s, 8s, max 5min)
- Logs processing history to `outbox_processing_logs`

**Processing Log Tracking:**
- Action (sent, failed, retried, moved_to_dlq)
- Status (success, pending, failed)
- Error message if any
- Processing time in milliseconds

---

## 6. NOTIFICATION PREFERENCE SYSTEM

### Domain Model
**File**: `/internal/modules/notification/domain/notification.go`

```go
type NotificationPreference struct {
    ID                 uuid.UUID      // Primary key
    UserID             uuid.UUID      // Unique per user
    EmailEnabled       bool           // Default: true
    SMSEnabled         bool           // Default: false
    PushEnabled        bool           // Default: false
    InAppEnabled       bool           // Default: true
    EmailFrequency     string         // "immediate", "daily", "weekly"
    UnsubscribedTopics string         // JSON array of topic names
    QuietHoursStart    *time.Time     // Optional: do not disturb time
    QuietHoursEnd      *time.Time
    Timezone           string         // Default: UTC
    Language           string         // Default: "en"
}
```

### Preference Management

**NotificationService Methods:**
```go
GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error)
UpdateUserPreferences(userID uuid.UUID, pref *domain.NotificationPreference) error
```

**Behavior:**
- Auto-creates default preferences if not exist
- Checks before sending notification
- Returns error if channel disabled for user
- Supports future: email frequency (daily digest, weekly summary)
- Supports future: quiet hours (no notifications during sleep)
- Supports future: topic-based unsubscription

### Repository Interface
**File**: `/internal/modules/notification/repository/notification_repository.go`

```go
CreateUserPreferences(pref *domain.NotificationPreference) error
UpdateUserPreferences(pref *domain.NotificationPreference) error
DeleteUserPreferences(userID uuid.UUID) error
GetUserPreferences(userID uuid.UUID) (*domain.NotificationPreference, error)
```

---

## 7. BACKGROUND WORKERS & JOBS

### Current Implementation

**File**: `/internal/modules/notification/service/notification_service.go`

**Methods Designed for Background Execution:**
```go
ProcessPendingNotifications(limit int) error
RetryFailedNotifications(limit int) error
GetScheduledNotifications(limit int) error
```

### ProcessPendingNotifications

**Logic:**
1. Query notifications with status = PENDING
2. Filter out scheduled notifications not yet due
3. Order by priority DESC, created_at ASC
4. Process up to `limit` (default 100) notifications
5. Spawn goroutine for each: `go s.processNotification()`

**Use Case:**
- Called periodically (e.g., every 5-10 seconds)
- Can be triggered from REST API for testing
- Primary mechanism for deferred notification sending

### RetryFailedNotifications

**Logic:**
1. Query notifications with status = FAILED
2. Filter by: retry_count < max_retries
3. Order by priority DESC, created_at ASC  
4. For each notification:
   - Increment retry_count
   - Reset status to PENDING
   - Save to DB
   - Spawn goroutine to process

**Use Case:**
- Called periodically (e.g., every minute)
- Retries up to 3 times (max_retries)
- Exponential backoff recommended (implement in caller)

### Scheduled Notifications

**Detection:**
- notifications where scheduled_at IS NOT NULL AND scheduled_at <= NOW()
- Status = PENDING

**Processing:**
- Handled by ProcessPendingNotifications (checks IsScheduled())
- Can also be specifically queried with GetScheduledNotifications()

### Current Gap
- **No built-in scheduler**: Code doesn't include cron/scheduler
- **Intended Integration**: External scheduler (apscheduler, Go cron libraries)
- **Future Enhancement**: Could use RabbitMQ delayed messages or Kafka scheduling

### Recommended Scheduler Integration

```go
// Pseudo-code for typical integration
func scheduleBackgroundJobs() {
    c := cron.New()
    
    // Every 5 seconds
    c.AddFunc("@every 5s", func() {
        notificationService.ProcessPendingNotifications(100)
    })
    
    // Every minute
    c.AddFunc("@every 1m", func() {
        notificationService.RetryFailedNotifications(50)
    })
    
    c.Start()
}
```

---

## 8. API ENDPOINTS

### Template Handler Routes
**File**: `/internal/modules/notification/api/template_handler.go`

**Base Route**: `/templates`

**CRUD:**
- POST `/` - Create new template
- GET `/` - List templates (pagination, filters)
- GET `/:id` - Retrieve single template
- PUT `/:id` - Update template
- DELETE `/:id` - Delete template

**Rendering:**
- POST `/render` - Render template with data
- POST `/preview` - Preview without saving

**Categories:**
- GET `/categories` - List all categories
- POST `/categories` - Create new category

**Analytics:**
- GET `/most-used` - Get top used templates

**System:**
- POST `/system/init` - Initialize default system templates

**Features:**
- Template cloning
- Bulk operations
- Import/export (structure present, implementation pending)

### Notification Service (No Direct Handler Shown)

**Implied Endpoints** (based on service methods):
- POST `/notifications/send` - Send notification
- POST `/notifications/email` - Send email
- GET `/notifications/:id` - Get notification
- GET `/notifications/user/:userId` - List user notifications
- GET `/notifications/:userId/preferences` - Get preferences
- PUT `/notifications/:userId/preferences` - Update preferences

---

## COMPLETE DATA FLOW EXAMPLE: USER REGISTRATION → VERIFICATION EMAIL

### Step-by-Step Flow

```
1. USER REGISTRATION
   ├─→ AuthService.Register(email, username)
   ├─→ Create User in DB
   ├─→ Generate verification token
   ├─→ Dispatch event: EventUserRegistered
   │   ├─→ EventDispatcher.DispatchUserRegistered()
   │   ├─→ Create DomainEvent (user.registered)
   │   ├─→ Call local handlers (async)
   │   ├─→ Convert to RabbitMQ Message
   │   ├─→ rabbitmq.PublishMessage()
   │   │   ├─→ Serialize message
   │   │   ├─→ Save to OutboxMessage table (PENDING)
   │   │   └─→ Return success immediately
   │   └─→ Local handler: Send verification email
   │
   └─→ Return to client

2. OUTBOX PROCESSOR (Background, every 5 seconds)
   ├─→ Query OutboxMessage where status = PENDING
   ├─→ For user.registered event:
   │   ├─→ Mark as PROCESSING
   │   ├─→ Publish to RabbitMQ
   │   │   ├─→ Connect to RabbitMQ
   │   │   ├─→ Publish to 'go-core' exchange
   │   │   └─→ Routing key: "user.registered"
   │   ├─→ On success: Mark as SENT
   │   └─→ Log to OutboxProcessingLog

3. DIRECT EMAIL SENDING (May also happen immediately)
   ├─→ NotificationService.SendEmail(SendEmailRequest)
   │   ├─→ Check user preferences (EmailEnabled)
   │   ├─→ Create Notification record (PENDING)
   │   ├─→ If immediate: spawn goroutine
   │   │
   │   ├─→ processEmailNotification()
   │   │   ├─→ Update status: PROCESSING
   │   │   ├─→ Get EnhancedEmailService
   │   │   ├─→ enhanc EmailService.SendWithTemplate()
   │   │   │   ├─→ Validate request
   │   │   │   ├─→ TemplateService.RenderTemplate()
   │   │   │   │   ├─→ GetTemplateByName("user_verification")
   │   │   │   │   ├─→ Get "en" language variant
   │   │   │   │   ├─→ Validate vars: Username, VerificationURL, etc.
   │   │   │   │   ├─→ Render using html/template
   │   │   │   │   ├─→ IncrementUsage() async
   │   │   │   │   └─→ Return RenderedTemplate
   │   │   │   ├─→ Create gomail.Message
   │   │   │   ├─→ Set From, To, Subject, Body
   │   │   │   ├─→ Set X-Priority headers
   │   │   │   ├─→ Set custom headers (X-Template: user_verification)
   │   │   │   ├─→ dialer.DialAndSend()
   │   │   │   └─→ Log "Email sent" to logger
   │   │   │
   │   │   ├─→ Create EmailLog record
   │   │   │   ├─→ from: system@app.com
   │   │   │   ├─→ to: user@email.com
   │   │   │   ├─→ subject: "Verify Your Email Address"
   │   │   │   ├─→ status: "sent"
   │   │   │   ├─→ notification_id: ref to Notification
   │   │   │   └─→ Save to email_logs table
   │   │   │
   │   │   ├─→ Update Notification
   │   │   │   ├─→ status: SENT
   │   │   │   ├─→ sent_at: NOW()
   │   │   │   └─→ Save to database
   │   │   │
   │   │   └─→ Handle errors
   │   │       ├─→ Mark Notification as FAILED
   │   │       ├─→ Store error message
   │   │       └─→ EmailLog status: "failed"
   │
   └─→ Return to API caller

4. EMAIL DELIVERY TO SMTP SERVER
   ├─→ gomail dialer connects to SMTP
   ├─→ Sends SMTP commands
   ├─→ SMTP server accepts and queues
   └─→ Server delivers to user's mailbox

5. RETRY MECHANISM (if failed)
   ├─→ RetryFailedNotifications() runs
   ├─→ Finds Notification with status=FAILED, retry_count < 3
   ├─→ Increments retry_count
   ├─→ Sets status back to PENDING
   ├─→ Spawns goroutine to retry
   └─→ Repeats from step 3 (email sending)
```

---

## DATABASE SCHEMA (Key Tables)

### notifications
```sql
id (UUID, PK)
user_id (UUID, FK to users)
type (email, sms, push, in_app, webhook)
status (pending, processing, sent, failed, cancelled, bounced)
priority (low, normal, high, urgent)
subject (text)
content (text)
template (name reference)
recipients (JSON array of emails)
metadata (JSONB with template vars, device tokens, webhook URL, etc.)
scheduled_at (nullable timestamp)
sent_at (nullable timestamp)
failed_at (nullable timestamp)
error (text)
retry_count (int, default 0)
max_retries (int, default 3)
created_at, updated_at, deleted_at
```

### notification_templates
```sql
id (UUID, PK)
name (varchar, unique)
type (email, sms, push, in_app, webhook)
subject (text)
body (text, contains {{variable}} references)
variables (JSONB array of required variables)
is_active (boolean, default true)
is_system (boolean, blocks deletion)
description (text)
version (int, default 1)
usage_count (int)
last_used_at (timestamp)
category_id (UUID, FK)
tags (JSONB array)
created_at, updated_at, deleted_at
```

### template_languages
```sql
id (UUID, PK)
template_id (UUID, FK)
language_code (varchar: en, tr, es, etc.)
subject (text, language-specific)
body (text, language-specific)
is_default (boolean)
created_at, updated_at
```

### template_variables
```sql
id (UUID, PK)
template_id (UUID, FK)
name (string, variable name)
type (string, number, boolean, date)
required (boolean)
default_value (text)
description (text)
created_at, updated_at
```

### notification_preferences
```sql
id (UUID, PK)
user_id (UUID, unique, FK)
email_enabled (boolean, default true)
sms_enabled (boolean, default false)
push_enabled (boolean, default false)
in_app_enabled (boolean, default true)
email_frequency (immediate, daily, weekly)
unsubscribed_topics (JSONB array)
quiet_hours_start (time)
quiet_hours_end (time)
timezone (varchar, default UTC)
language (varchar, default en)
created_at, updated_at, deleted_at
```

### email_logs
```sql
id (UUID, PK)
notification_id (UUID, FK, nullable)
from (email address)
to (email address)
cc (email addresses)
bcc (email addresses)
subject (text)
body (text)
template (template name reference)
status (pending, sent, failed)
smtp_response (text)
message_id (unique ID from SMTP)
error (text)
opened_at (timestamp, nullable)
clicked_at (timestamp, nullable)
bounced_at (timestamp, nullable)
unsubscribed_at (timestamp, nullable)
created_at, updated_at
```

### outbox_messages
```sql
id (UUID, PK)
aggregate_id (UUID, FK to aggregate - notification, user, etc.)
aggregate_type (Notification, User, etc.)
event_type (notification.sent, email.sent, etc.)
payload (JSONB, full message)
status (pending, processing, sent, failed, dlq)
queue (exchange name)
routing_key (RabbitMQ routing key)
priority (0-9)
retry_count (int)
max_retries (int, default 3)
next_retry_at (timestamp)
processed_at (timestamp)
failed_at (timestamp)
error (text)
correlation_id (UUID, for tracing)
causation_id (UUID, what caused this)
ttl (seconds, 0 = no expiry)
created_at, updated_at, deleted_at
```

### outbox_dead_letters
```sql
id (UUID, PK)
outbox_message_id (UUID, FK)
original_message (JSONB, full message copy)
failure_reason (text)
retry_count (int)
last_error (text)
queue (varchar)
event_type (varchar, indexed)
reprocessed (boolean)
reprocessed_at (timestamp)
notes (text, manual debugging notes)
created_at
```

### outbox_processing_logs
```sql
id (UUID, PK)
outbox_message_id (UUID, FK, indexed)
action (sent, failed, retried, moved_to_dlq)
status (success, pending, failed)
error (text)
processing_time (int64, milliseconds)
created_at
```

---

## KEY DESIGN PATTERNS

### 1. Repository Pattern
- Abstracts database operations
- NotificationRepository interface defines contracts
- Implementation uses GORM ORM
- Supports both basic queries and complex filters

### 2. Service Layer Pattern
- NotificationService for orchestration
- TemplateService for template management
- EmailService for SMTP operations
- EnhancedEmailService adds database template support
- Clean separation of concerns

### 3. Domain-Driven Design
- Rich domain models (Notification, Template, Preference)
- Business logic in domain methods (IsScheduled(), CanRetry(), MarkAsSent())
- Event sourcing foundations (event types, domain events)

### 4. Transactional Outbox Pattern
- Solves distributed transaction problem
- Guarantees at-least-once delivery
- Database transaction includes both business data and event
- Separate processor handles async publishing
- Dead letter queue for failures

### 5. Event-Driven Architecture
- DomainEvent base type for all events
- EventDispatcher for routing
- Local handlers for in-process listeners
- RabbitMQ for inter-service communication
- CorrelationID for distributed tracing

### 6. Dependency Injection
- Services receive dependencies in constructor
- Config injected from application bootstrap
- Repositories injected into services
- Logger integrated throughout

---

## ERROR HANDLING & RESILIENCE

### Retry Logic
- **Notification Level**: Built-in retry_count / max_retries
- **Outbox Level**: Exponential backoff, max retries before DLQ
- **RabbitMQ Level**: Message TTL (5 minutes), dead letter exchange
- **Email SMTP**: gomail handles connection retries

### Failure Scenarios

| Scenario | Handling |
|----------|----------|
| SMTP unreachable | Email marked FAILED, can retry |
| Template not found | Return error, don't save notification |
| User disabled channel | Service returns BadRequest error |
| RabbitMQ down | Outbox persists, retries when back |
| Maximum retries exceeded | Moved to DLQ for manual review |
| Database connection lost | Handled by GORM connection pooling |
| Template rendering error | Logged, notification marked failed |

### Monitoring Points
- EmailLog records all sends (success/failure)
- OutboxProcessingLog tracks delivery attempts
- NotificationService updates status after each attempt
- Logger integration for debugging

---

## SECURITY CONSIDERATIONS

### Implemented
- Soft deletes on all sensitive data
- UUID primary keys (not sequential)
- JSONB fields for flexible, secure storage
- SMTP TLS configuration
- Error messages don't leak sensitive info
- User preferences prevent unwanted notifications

### Recommendations for Production
- Rate limiting on notification creation endpoints
- Signature verification for incoming webhooks
- Audit logging for preference changes
- Encryption for stored phone numbers
- Template syntax validation (prevent injection)
- Authentication on admin endpoints (template init)
- Sanitization of user-provided template data

---

## PERFORMANCE CONSIDERATIONS

### Optimizations in Place
- **Indexes**: UserID, NotificationType, Status on notifications
- **Batch Processing**: ProcessPendingNotifications(limit)
- **Async Goroutines**: Non-blocking notification dispatch
- **Usage Tracking**: Queries most-used templates efficiently
- **Outbox Batching**: Processes multiple messages per cycle
- **Connection Pooling**: RabbitMQ connection reuse
- **Pagination**: List operations support pagination

### Scaling Recommendations
1. **Horizontal**: Multiple instances of background processors
2. **Database**: Add read replicas for analytics queries
3. **RabbitMQ**: Multiple consumers per queue
4. **Caching**: Cache templates in memory with TTL
5. **Async Email**: Consider queue for SMTP operations
6. **Indexing**: Add composites (user_id, created_at, status)

---

## SUMMARY TABLE

| Component | Technology | Purpose |
|-----------|-----------|---------|
| API Framework | Fiber | HTTP server for endpoints |
| ORM | GORM | Database operations |
| Database | PostgreSQL | Persistent storage |
| Email | gomail | SMTP delivery |
| Message Queue | RabbitMQ | Event pub/sub |
| Templates | Go html/template | Dynamic content rendering |
| Logging | Custom Logger | Observability |
| Unique IDs | google/uuid | Primary keys |

---

## INTEGRATION POINTS FOR EXTERNAL SYSTEMS

1. **SMS Provider**: Modify sendSMSNotification() to call Twilio/SNS API
2. **Push Service**: Modify sendPushNotification() for FCM/APNs
3. **Webhook Provider**: Add retry logic and signature generation
4. **Analytics**: Subscribe to event.sent / email.opened events
5. **Email Tracking**: Parse bounce/complaint webhooks from SMTP provider
6. **Scheduler**: Call ProcessPendingNotifications() and RetryFailedNotifications()
7. **Authentication**: Check user ID for preference management
8. **Multi-tenancy**: Use TenantID in outbox events if multi-tenant

---

## CONCLUSION

This notification system provides a robust, production-ready architecture supporting:
- **Multiple delivery channels** (email, SMS, push, in-app, webhooks)
- **Template management** with multi-language support
- **User preferences** for consent and frequency control
- **Reliable delivery** via transactional outbox pattern + RabbitMQ
- **Event-driven** integration with rest of application
- **Comprehensive tracking** via logs and event history
- **Retry mechanisms** with exponential backoff
- **Database schema** with proper relationships and constraints

The system is designed for scalability, maintainability, and reliability in production environments.
