-- +goose Up

-- Trigger function: notify on outbox changes
CREATE OR REPLACE FUNCTION notify_outbox_new_message()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('outbox_new_message', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger: fire on INSERT when status = 'pending'
CREATE TRIGGER trg_outbox_new_message
    AFTER INSERT ON outbox_messages
    FOR EACH ROW
    WHEN (NEW.status = 'pending')
    EXECUTE FUNCTION notify_outbox_new_message();

-- Trigger: fire on UPDATE when status transitions to 'pending' (DLQ reprocess)
CREATE TRIGGER trg_outbox_reprocess_message
    AFTER UPDATE ON outbox_messages
    FOR EACH ROW
    WHEN (OLD.status != 'pending' AND NEW.status = 'pending')
    EXECUTE FUNCTION notify_outbox_new_message();

-- Partial composite index for pending message polling (priority DESC, created_at ASC)
CREATE INDEX idx_outbox_messages_pending_poll
    ON outbox_messages (priority DESC, created_at ASC)
    WHERE status = 'pending' AND deleted_at IS NULL;

-- Partial composite index for failed message retry polling (next_retry_at ASC, priority DESC)
CREATE INDEX idx_outbox_messages_retry_poll
    ON outbox_messages (next_retry_at ASC, priority DESC)
    WHERE status = 'failed' AND deleted_at IS NULL;

-- +goose Down

DROP TRIGGER IF EXISTS trg_outbox_reprocess_message ON outbox_messages;
DROP TRIGGER IF EXISTS trg_outbox_new_message ON outbox_messages;
DROP FUNCTION IF EXISTS notify_outbox_new_message();
DROP INDEX IF EXISTS idx_outbox_messages_pending_poll;
DROP INDEX IF EXISTS idx_outbox_messages_retry_poll;
