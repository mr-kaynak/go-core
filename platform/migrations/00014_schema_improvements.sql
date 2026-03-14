-- +goose Up

-- FK constraint on email_logs.notification_id for referential integrity
ALTER TABLE email_logs
    ADD CONSTRAINT fk_email_logs_notification_id
    FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE SET NULL;

-- Composite index for user+time range audit log queries
CREATE INDEX idx_audit_logs_user_created ON audit_logs(user_id, created_at);

-- CHECK constraint to enforce valid notification types at DB level
ALTER TABLE notifications
    ADD CONSTRAINT chk_notifications_type
    CHECK (type IN ('email', 'sms', 'push', 'in_app', 'webhook'));

-- Add soft-delete support to blog revision and media tables
ALTER TABLE blog_post_revisions ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_blog_post_revisions_deleted_at ON blog_post_revisions(deleted_at);

ALTER TABLE blog_post_media ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_blog_post_media_deleted_at ON blog_post_media(deleted_at);

-- +goose Down

ALTER TABLE blog_post_media DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE blog_post_revisions DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS chk_notifications_type;
DROP INDEX IF EXISTS idx_audit_logs_user_created;
ALTER TABLE email_logs DROP CONSTRAINT IF EXISTS fk_email_logs_notification_id;
