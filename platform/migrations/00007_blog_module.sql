-- +goose Up

-- Blog posts
CREATE TABLE blog_posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    excerpt VARCHAR(500),
    content_json JSONB,
    content_html TEXT,
    content_plain TEXT,
    cover_image_url VARCHAR(512),
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    author_id UUID NOT NULL REFERENCES users(id),
    category_id UUID,
    read_time INT DEFAULT 0,
    is_featured BOOLEAN DEFAULT FALSE,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_blog_posts_slug ON blog_posts(slug);
CREATE INDEX idx_blog_posts_author_id ON blog_posts(author_id);
CREATE INDEX idx_blog_posts_category_id ON blog_posts(category_id);
CREATE INDEX idx_blog_posts_status ON blog_posts(status);
CREATE INDEX idx_blog_posts_published_at ON blog_posts(published_at);
CREATE INDEX idx_blog_posts_deleted_at ON blog_posts(deleted_at);
CREATE INDEX idx_blog_posts_status_published ON blog_posts(status, published_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_blog_posts_fulltext ON blog_posts USING GIN (to_tsvector('english', coalesce(content_plain, '')));

-- Blog categories
CREATE TABLE blog_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    description VARCHAR(500),
    parent_id UUID REFERENCES blog_categories(id) ON DELETE SET NULL,
    sort_order INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE UNIQUE INDEX idx_blog_categories_slug ON blog_categories(slug);
CREATE INDEX idx_blog_categories_parent_id ON blog_categories(parent_id);

-- Add category FK to posts after categories table exists
ALTER TABLE blog_posts ADD CONSTRAINT fk_blog_posts_category FOREIGN KEY (category_id) REFERENCES blog_categories(id) ON DELETE SET NULL;

-- Blog tags
CREATE TABLE blog_tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE UNIQUE INDEX idx_blog_tags_slug ON blog_tags(slug);

-- Post-Tag join table
CREATE TABLE post_tags (
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES blog_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);

-- Blog comments
CREATE TABLE blog_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    parent_id UUID REFERENCES blog_comments(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    guest_name VARCHAR(100),
    guest_email VARCHAR(255),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_blog_comments_post_id ON blog_comments(post_id);
CREATE INDEX idx_blog_comments_author_id ON blog_comments(author_id);
CREATE INDEX idx_blog_comments_parent_id ON blog_comments(parent_id);
CREATE INDEX idx_blog_comments_status ON blog_comments(status);
CREATE INDEX idx_blog_comments_deleted_at ON blog_comments(deleted_at);

-- Blog post revisions
CREATE TABLE blog_post_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    editor_id UUID NOT NULL REFERENCES users(id),
    title VARCHAR(255) NOT NULL,
    content_json JSONB,
    content_html TEXT,
    excerpt VARCHAR(500),
    version INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_blog_post_revisions_post_id ON blog_post_revisions(post_id);

-- Blog post media
CREATE TABLE blog_post_media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    uploader_id UUID NOT NULL REFERENCES users(id),
    s3_key VARCHAR(512) NOT NULL,
    filename VARCHAR(255) NOT NULL,
    media_type VARCHAR(20) NOT NULL,
    content_type VARCHAR(100),
    file_size BIGINT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_blog_post_media_post_id ON blog_post_media(post_id);

-- Blog post likes
CREATE TABLE blog_post_likes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE UNIQUE INDEX idx_blog_post_likes_unique ON blog_post_likes(post_id, user_id);

-- Blog post views
CREATE TABLE blog_post_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ip_address VARCHAR(45),
    user_agent VARCHAR(512),
    referrer VARCHAR(512),
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_blog_post_views_post_id ON blog_post_views(post_id);
CREATE INDEX idx_blog_post_views_viewed_at ON blog_post_views(viewed_at);
CREATE INDEX idx_blog_post_views_dedup ON blog_post_views(post_id, ip_address, viewed_at);

-- Blog post shares
CREATE TABLE blog_post_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    platform VARCHAR(50) NOT NULL,
    ip_address VARCHAR(45),
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_blog_post_shares_post_id ON blog_post_shares(post_id);

-- Blog post stats (aggregated)
CREATE TABLE blog_post_stats (
    post_id UUID PRIMARY KEY REFERENCES blog_posts(id) ON DELETE CASCADE,
    like_count INT DEFAULT 0,
    view_count INT DEFAULT 0,
    share_count INT DEFAULT 0,
    comment_count INT DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS blog_post_stats;
DROP TABLE IF EXISTS blog_post_shares;
DROP TABLE IF EXISTS blog_post_views;
DROP TABLE IF EXISTS blog_post_likes;
DROP TABLE IF EXISTS blog_post_media;
DROP TABLE IF EXISTS blog_post_revisions;
DROP TABLE IF EXISTS blog_comments;
DROP TABLE IF EXISTS post_tags;
DROP TABLE IF EXISTS blog_tags;
ALTER TABLE blog_posts DROP CONSTRAINT IF EXISTS fk_blog_posts_category;
DROP TABLE IF EXISTS blog_posts;
DROP TABLE IF EXISTS blog_categories;
