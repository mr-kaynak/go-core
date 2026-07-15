-- +goose Up

-- Replace the full unique index on blog_posts.slug with a partial unique index
-- that excludes soft-deleted rows. Without this, a soft-deleted post
-- permanently blocks reuse of its slug by a new record.
--
-- This migration only touches blog_posts, which is soft-deleted (has a
-- deleted_at column, see 00007_blog_module.sql and
-- internal/modules/blog/domain/post.go). blog_categories and blog_tags are
-- hard-deleted by design — they have no deleted_at column at all (see
-- internal/modules/blog/domain/category.go and tag.go) — so the plain unique
-- indexes created for them in 00007 are already correct and need no
-- partial-index treatment. Attempting `WHERE deleted_at IS NULL` on those
-- tables fails with "column deleted_at does not exist" on a fresh database.

DROP INDEX IF EXISTS idx_blog_posts_slug;

CREATE UNIQUE INDEX idx_blog_posts_slug ON blog_posts(slug) WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_blog_posts_slug;

CREATE UNIQUE INDEX idx_blog_posts_slug ON blog_posts(slug);
