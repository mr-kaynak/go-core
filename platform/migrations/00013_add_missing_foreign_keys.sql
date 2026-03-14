-- +goose Up

-- Add missing FK constraints for referential integrity on tables that
-- reference users but had no database-level enforcement.

ALTER TABLE api_keys
    ADD CONSTRAINT fk_api_keys_user_id
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE notifications
    ADD CONSTRAINT fk_notifications_user_id
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE notification_preferences
    ADD CONSTRAINT fk_notification_preferences_user_id
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- +goose Down

ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS fk_api_keys_user_id;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS fk_notifications_user_id;
ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS fk_notification_preferences_user_id;
