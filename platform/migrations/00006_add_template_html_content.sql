-- Add html_content column to notification_templates for full HTML email support
ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS html_content TEXT;

-- Add html_content column to template_languages for per-language HTML overrides
ALTER TABLE template_languages ADD COLUMN IF NOT EXISTS html_content TEXT;

COMMENT ON COLUMN notification_templates.html_content IS 'Full HTML document template for email rendering. When present, used instead of body for email sending.';
COMMENT ON COLUMN template_languages.html_content IS 'Full HTML document template override for this language variant.';
