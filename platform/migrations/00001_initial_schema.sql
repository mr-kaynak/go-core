-- +goose Up

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    password VARCHAR(255) NOT NULL,
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    phone VARCHAR(255),
    status VARCHAR(20) DEFAULT 'pending',
    verified BOOLEAN DEFAULT false,
    last_login TIMESTAMPTZ,
    failed_login_attempts INTEGER DEFAULT 0,
    locked_until TIMESTAMPTZ,
    two_factor_secret VARCHAR(64),
    two_factor_enabled BOOLEAN DEFAULT false,
    two_factor_backup_codes TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE UNIQUE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);

-- Roles table
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_roles_name ON roles(name);
CREATE INDEX idx_roles_deleted_at ON roles(deleted_at);

-- Permissions table
CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description VARCHAR(255),
    category VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_permissions_name ON permissions(name);
CREATE INDEX idx_permissions_category ON permissions(category);
CREATE INDEX idx_permissions_deleted_at ON permissions(deleted_at);

-- User-Role join table
CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

-- Role-Permission join table
CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (role_id, permission_id)
);

-- Refresh tokens table
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE UNIQUE INDEX idx_refresh_tokens_token ON refresh_tokens(token);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);

-- Verification tokens table
CREATE TABLE verification_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) NOT NULL,
    type VARCHAR(30),
    used BOOLEAN DEFAULT false,
    used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_verification_tokens_token ON verification_tokens(token);
CREATE INDEX idx_verification_tokens_user_id ON verification_tokens(user_id);
CREATE INDEX idx_verification_tokens_deleted_at ON verification_tokens(deleted_at);

-- Audit logs table
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID,
    action VARCHAR(255),
    resource VARCHAR(255),
    resource_id VARCHAR(255),
    ip_address VARCHAR(255),
    user_agent VARCHAR(255),
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);

-- API keys table
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    key_prefix VARCHAR(8),
    name VARCHAR(255),
    scopes TEXT,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX idx_api_keys_deleted_at ON api_keys(deleted_at);

-- Notifications table
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID,
    type VARCHAR(20),
    status VARCHAR(20) DEFAULT 'pending',
    priority VARCHAR(20) DEFAULT 'normal',
    subject VARCHAR(255),
    content TEXT,
    template VARCHAR(255),
    recipients VARCHAR(255),
    metadata JSONB,
    scheduled_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    error VARCHAR(255),
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_notifications_user_id ON notifications(user_id);
CREATE INDEX idx_notifications_deleted_at ON notifications(deleted_at);

-- Email logs table
CREATE TABLE email_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID,
    from_addr VARCHAR(255),
    to_addr VARCHAR(255),
    cc VARCHAR(255),
    bcc VARCHAR(255),
    subject VARCHAR(255),
    body TEXT,
    template VARCHAR(255),
    status VARCHAR(20) DEFAULT 'pending',
    smtp_response VARCHAR(255),
    message_id VARCHAR(255),
    error VARCHAR(255),
    opened_at TIMESTAMPTZ,
    clicked_at TIMESTAMPTZ,
    bounced_at TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_email_logs_notification_id ON email_logs(notification_id);

-- Template categories table
CREATE TABLE template_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description VARCHAR(255),
    parent_id UUID REFERENCES template_categories(id),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_template_categories_name ON template_categories(name);
CREATE INDEX idx_template_categories_deleted_at ON template_categories(deleted_at);

-- Notification templates table
CREATE TABLE notification_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    type VARCHAR(20),
    subject VARCHAR(255),
    body TEXT,
    variables JSONB,
    is_active BOOLEAN DEFAULT true,
    description VARCHAR(255),
    category_id UUID REFERENCES template_categories(id),
    tags JSONB,
    version INTEGER DEFAULT 1,
    is_system BOOLEAN DEFAULT false,
    last_used_at TIMESTAMPTZ,
    usage_count INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_notification_templates_name ON notification_templates(name);
CREATE INDEX idx_notification_templates_category_id ON notification_templates(category_id);
CREATE INDEX idx_notification_templates_deleted_at ON notification_templates(deleted_at);

-- Template languages table
CREATE TABLE template_languages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    language_code VARCHAR(10),
    subject VARCHAR(255),
    body TEXT,
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_template_languages_template_id ON template_languages(template_id);
CREATE INDEX idx_template_languages_language_code ON template_languages(language_code);

-- Template variables table
CREATE TABLE template_variables (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    name VARCHAR(255),
    type VARCHAR(255) DEFAULT 'string',
    required BOOLEAN DEFAULT true,
    default_value VARCHAR(255),
    description VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_template_variables_template_id ON template_variables(template_id);

-- Notification preferences table
CREATE TABLE notification_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    email_enabled BOOLEAN DEFAULT true,
    sms_enabled BOOLEAN DEFAULT false,
    push_enabled BOOLEAN DEFAULT false,
    in_app_enabled BOOLEAN DEFAULT true,
    email_frequency VARCHAR(20) DEFAULT 'immediate',
    unsubscribed_topics JSONB,
    quiet_hours_start VARCHAR(255),
    quiet_hours_end VARCHAR(255),
    timezone VARCHAR(255) DEFAULT 'UTC',
    language VARCHAR(255) DEFAULT 'en',
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_notification_preferences_user_id ON notification_preferences(user_id);
CREATE INDEX idx_notification_preferences_deleted_at ON notification_preferences(deleted_at);

-- Outbox messages table
CREATE TABLE outbox_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id UUID,
    aggregate_type VARCHAR(100),
    event_type VARCHAR(100),
    event_version INTEGER DEFAULT 1,
    payload JSONB,
    metadata JSONB,
    status VARCHAR(20) DEFAULT 'pending',
    queue VARCHAR(100),
    routing_key VARCHAR(100),
    priority INTEGER DEFAULT 0,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    next_retry_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    error TEXT,
    correlation_id VARCHAR(100),
    causation_id VARCHAR(100),
    user_id UUID,
    tenant_id UUID,
    ttl INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_messages_aggregate_id ON outbox_messages(aggregate_id);
CREATE INDEX idx_outbox_messages_aggregate_type ON outbox_messages(aggregate_type);
CREATE INDEX idx_outbox_messages_event_type ON outbox_messages(event_type);
CREATE INDEX idx_outbox_messages_status ON outbox_messages(status);
CREATE INDEX idx_outbox_messages_queue ON outbox_messages(queue);
CREATE INDEX idx_outbox_messages_correlation_id ON outbox_messages(correlation_id);
CREATE INDEX idx_outbox_messages_deleted_at ON outbox_messages(deleted_at);

-- Outbox dead letters table
CREATE TABLE outbox_dead_letters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_message_id UUID,
    original_message JSONB,
    failure_reason TEXT,
    retry_count INTEGER,
    last_error TEXT,
    queue VARCHAR(100),
    event_type VARCHAR(100),
    reprocessed BOOLEAN DEFAULT false,
    reprocessed_at TIMESTAMPTZ,
    notes TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_outbox_dead_letters_outbox_message_id ON outbox_dead_letters(outbox_message_id);
CREATE INDEX idx_outbox_dead_letters_event_type ON outbox_dead_letters(event_type);

-- Outbox processing logs table
CREATE TABLE outbox_processing_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    outbox_message_id UUID,
    action VARCHAR(50),
    status VARCHAR(20),
    error TEXT,
    processing_time BIGINT,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_outbox_processing_logs_outbox_message_id ON outbox_processing_logs(outbox_message_id);

-- +goose Down

DROP TABLE IF EXISTS outbox_processing_logs;
DROP TABLE IF EXISTS outbox_dead_letters;
DROP TABLE IF EXISTS outbox_messages;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS template_variables;
DROP TABLE IF EXISTS template_languages;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS template_categories;
DROP TABLE IF EXISTS email_logs;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS verification_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
