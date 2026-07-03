-- +goose Up

-- Replace full unique indexes on blog slug columns with partial unique indexes
-- that exclude soft-deleted rows. Without this, a soft-deleted post, category,
-- or tag permanently blocks reuse of its slug by a new record.

DROP INDEX IF EXISTS idx_blog_posts_slug;
DROP INDEX IF EXISTS idx_blog_categories_slug;
DROP INDEX IF EXISTS idx_blog_tags_slug;

CREATE UNIQUE INDEX idx_blog_posts_slug ON blog_posts(slug) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_blog_categories_slug ON blog_categories(slug) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_blog_tags_slug ON blog_tags(slug) WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_blog_posts_slug;
DROP INDEX IF EXISTS idx_blog_categories_slug;
DROP INDEX IF EXISTS idx_blog_tags_slug;

CREATE UNIQUE INDEX idx_blog_posts_slug ON blog_posts(slug);
CREATE UNIQUE INDEX idx_blog_categories_slug ON blog_categories(slug);
CREATE UNIQUE INDEX idx_blog_tags_slug ON blog_tags(slug);
