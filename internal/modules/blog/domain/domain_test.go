package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	notificationDomain "github.com/mr-kaynak/go-core/internal/modules/notification/domain"
)

// ---------------------------------------------------------------------------
// ContentJSON
// ---------------------------------------------------------------------------

func TestContentJSONValue(t *testing.T) {
	tests := []struct {
		name        string
		input       ContentJSON
		expectNil   bool
		expectBytes []byte
	}{
		{
			name:      "nil returns nil driver value",
			input:     nil,
			expectNil: true,
		},
		{
			name:        "non-nil returns raw bytes",
			input:       ContentJSON(`{"type":"doc"}`),
			expectBytes: []byte(`{"type":"doc"}`),
		},
		{
			name:        "empty but non-nil slice",
			input:       ContentJSON([]byte{}),
			expectBytes: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expectNil {
				if val != nil {
					t.Errorf("expected nil driver.Value, got %v", val)
				}
				return
			}
			b, ok := val.([]byte)
			if !ok {
				t.Fatalf("expected []byte driver.Value, got %T", val)
			}
			if string(b) != string(tt.expectBytes) {
				t.Errorf("expected %q, got %q", tt.expectBytes, b)
			}
		})
	}
}

func TestContentJSONScan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected ContentJSON
	}{
		{
			name:     "nil input sets nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "non-type-assertable value sets nil",
			input:    42,
			expected: nil,
		},
		{
			name:     "valid bytes are copied",
			input:    []byte(`{"type":"doc"}`),
			expected: ContentJSON(`{"type":"doc"}`),
		},
		{
			name:     "empty bytes",
			input:    []byte{},
			expected: ContentJSON{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c ContentJSON
			if err := c.Scan(tt.input); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(c) != string(tt.expected) {
				t.Errorf("expected %q, got %q", tt.expected, c)
			}
		})
	}
}

func TestContentJSONScanIsolation(t *testing.T) {
	// Ensure Scan copies rather than aliases the source slice.
	src := []byte(`{"key":"value"}`)
	var c ContentJSON
	if err := c.Scan(src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate source after scan.
	src[1] = 'X'
	if c[1] == 'X' {
		t.Errorf("Scan should have copied bytes, not aliased them")
	}
}

func TestContentJSONMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    ContentJSON
		expected string
	}{
		{
			name:     "nil marshals to null",
			input:    nil,
			expected: "null",
		},
		{
			name:     "valid JSON passes through",
			input:    ContentJSON(`{"type":"doc","children":[]}`),
			expected: `{"type":"doc","children":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.input.MarshalJSON()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(out) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, out)
			}
		})
	}
}

func TestContentJSONUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ContentJSON
	}{
		{
			name:     "null string sets nil",
			input:    "null",
			expected: nil,
		},
		{
			name:     "valid JSON object",
			input:    `{"type":"doc"}`,
			expected: ContentJSON(`{"type":"doc"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c ContentJSON
			if err := c.UnmarshalJSON([]byte(tt.input)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(c) != string(tt.expected) {
				t.Errorf("expected %q, got %q", tt.expected, c)
			}
		})
	}
}

func TestContentJSONRoundTrip(t *testing.T) {
	// Marshal through json.Marshal then unmarshal back.
	original := ContentJSON(`{"nodes":[{"type":"paragraph","text":"hello"}]}`)

	type wrapper struct {
		Content ContentJSON `json:"content"`
	}

	encoded, err := json.Marshal(wrapper{Content: original})
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded wrapper
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if string(decoded.Content) != string(original) {
		t.Errorf("round-trip mismatch: expected %q, got %q", original, decoded.Content)
	}
}

// ---------------------------------------------------------------------------
// Post
// ---------------------------------------------------------------------------

func TestPostTableName(t *testing.T) {
	p := Post{}
	if got := p.TableName(); got != "blog_posts" {
		t.Errorf("expected table name %q, got %q", "blog_posts", got)
	}
}

func TestPostBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		p := &Post{}
		if p.ID != uuid.Nil {
			t.Fatalf("expected nil UUID before BeforeCreate")
		}
		if err := p.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if p.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		p := &Post{ID: existing}
		if err := p.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if p.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, p.ID)
		}
	})
}

func TestPostIsPublished(t *testing.T) {
	tests := []struct {
		name      string
		status    PostStatus
		published bool
	}{
		{"published status", PostStatusPublished, true},
		{"draft status", PostStatusDraft, false},
		{"archived status", PostStatusArchived, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Post{Status: tt.status}
			if got := p.IsPublished(); got != tt.published {
				t.Errorf("IsPublished() = %v, want %v", got, tt.published)
			}
		})
	}
}

func TestPostIsDraft(t *testing.T) {
	tests := []struct {
		name    string
		status  PostStatus
		isDraft bool
	}{
		{"draft status", PostStatusDraft, true},
		{"published status", PostStatusPublished, false},
		{"archived status", PostStatusArchived, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Post{Status: tt.status}
			if got := p.IsDraft(); got != tt.isDraft {
				t.Errorf("IsDraft() = %v, want %v", got, tt.isDraft)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Category
// ---------------------------------------------------------------------------

func TestCategoryTableName(t *testing.T) {
	c := Category{}
	if got := c.TableName(); got != "blog_categories" {
		t.Errorf("expected table name %q, got %q", "blog_categories", got)
	}
}

func TestCategoryBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		c := &Category{}
		if c.ID != uuid.Nil {
			t.Fatalf("expected nil UUID before BeforeCreate")
		}
		if err := c.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if c.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		c := &Category{ID: existing}
		if err := c.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if c.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, c.ID)
		}
	})
}

func TestCategoryIsRoot(t *testing.T) {
	t.Run("root category has nil ParentID", func(t *testing.T) {
		c := &Category{ParentID: nil}
		if !c.IsRoot() {
			t.Errorf("expected IsRoot() = true for nil ParentID")
		}
	})

	t.Run("non-root category has non-nil ParentID", func(t *testing.T) {
		parentID := uuid.New()
		c := &Category{ParentID: &parentID}
		if c.IsRoot() {
			t.Errorf("expected IsRoot() = false for non-nil ParentID")
		}
	})
}

// ---------------------------------------------------------------------------
// Tag
// ---------------------------------------------------------------------------

func TestTagTableName(t *testing.T) {
	tag := Tag{}
	if got := tag.TableName(); got != "blog_tags" {
		t.Errorf("expected table name %q, got %q", "blog_tags", got)
	}
}

func TestTagBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		tag := &Tag{}
		if tag.ID != uuid.Nil {
			t.Fatalf("expected nil UUID before BeforeCreate")
		}
		if err := tag.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if tag.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		tag := &Tag{ID: existing}
		if err := tag.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if tag.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, tag.ID)
		}
	})
}

// ---------------------------------------------------------------------------
// Comment
// ---------------------------------------------------------------------------

func TestCommentTableName(t *testing.T) {
	c := Comment{}
	if got := c.TableName(); got != "blog_comments" {
		t.Errorf("expected table name %q, got %q", "blog_comments", got)
	}
}

func TestCommentBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		c := &Comment{}
		if c.ID != uuid.Nil {
			t.Fatalf("expected nil UUID before BeforeCreate")
		}
		if err := c.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if c.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		c := &Comment{ID: existing}
		if err := c.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if c.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, c.ID)
		}
	})
}

func TestCommentIsGuest(t *testing.T) {
	t.Run("guest comment has nil AuthorID", func(t *testing.T) {
		c := &Comment{AuthorID: nil, GuestName: "Alice"}
		if !c.IsGuest() {
			t.Errorf("expected IsGuest() = true for nil AuthorID")
		}
	})

	t.Run("authenticated comment has non-nil AuthorID", func(t *testing.T) {
		authorID := uuid.New()
		c := &Comment{AuthorID: &authorID}
		if c.IsGuest() {
			t.Errorf("expected IsGuest() = false for non-nil AuthorID")
		}
	})
}

func TestCommentIsRoot(t *testing.T) {
	t.Run("root comment has nil ParentID", func(t *testing.T) {
		c := &Comment{ParentID: nil}
		if !c.IsRoot() {
			t.Errorf("expected IsRoot() = true for nil ParentID")
		}
	})

	t.Run("reply comment has non-nil ParentID", func(t *testing.T) {
		parentID := uuid.New()
		c := &Comment{ParentID: &parentID}
		if c.IsRoot() {
			t.Errorf("expected IsRoot() = false for non-nil ParentID")
		}
	})
}

// ---------------------------------------------------------------------------
// PostStats
// ---------------------------------------------------------------------------

func TestPostStatsTableName(t *testing.T) {
	ps := PostStats{}
	if got := ps.TableName(); got != "blog_post_stats" {
		t.Errorf("expected table name %q, got %q", "blog_post_stats", got)
	}
}

func TestPostStatsAggregationFields(t *testing.T) {
	postID := uuid.New()
	now := time.Now()
	stats := PostStats{
		PostID:       postID,
		LikeCount:    42,
		ViewCount:    1000,
		ShareCount:   15,
		CommentCount: 7,
		UpdatedAt:    now,
	}

	if stats.PostID != postID {
		t.Errorf("expected PostID %v, got %v", postID, stats.PostID)
	}
	if stats.LikeCount != 42 {
		t.Errorf("expected LikeCount 42, got %d", stats.LikeCount)
	}
	if stats.ViewCount != 1000 {
		t.Errorf("expected ViewCount 1000, got %d", stats.ViewCount)
	}
	if stats.ShareCount != 15 {
		t.Errorf("expected ShareCount 15, got %d", stats.ShareCount)
	}
	if stats.CommentCount != 7 {
		t.Errorf("expected CommentCount 7, got %d", stats.CommentCount)
	}
}

// ---------------------------------------------------------------------------
// Post.CanTransition
// ---------------------------------------------------------------------------

func TestPostCanTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     PostStatus
		to       PostStatus
		expected bool
	}{
		{"draft to published is valid", PostStatusDraft, PostStatusPublished, true},
		{"published to archived is valid", PostStatusPublished, PostStatusArchived, true},
		{"draft to archived is invalid", PostStatusDraft, PostStatusArchived, false},
		{"draft to draft is invalid", PostStatusDraft, PostStatusDraft, false},
		{"published to draft is valid", PostStatusPublished, PostStatusDraft, true},
		{"published to published is invalid", PostStatusPublished, PostStatusPublished, false},
		{"archived to draft is valid", PostStatusArchived, PostStatusDraft, true},
		{"archived to published is invalid", PostStatusArchived, PostStatusPublished, false},
		{"archived to archived is invalid", PostStatusArchived, PostStatusArchived, false},
		{"unknown status has no valid transitions", PostStatus("unknown"), PostStatusPublished, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Post{Status: tt.from}
			if got := p.CanTransition(tt.to); got != tt.expected {
				t.Errorf("CanTransition(%q -> %q) = %v, want %v", tt.from, tt.to, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContentJSON.ToJSON
// ---------------------------------------------------------------------------

func TestContentJSONToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    ContentJSON
		expected string
	}{
		{"nil returns null", nil, "null"},
		{"valid JSON passes through", ContentJSON(`{"type":"doc"}`), `{"type":"doc"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.ToJSON()
			if string(got) != tt.expected {
				t.Errorf("ToJSON() = %q, want %q", string(got), tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PostLike
// ---------------------------------------------------------------------------

func TestPostLikeTableName(t *testing.T) {
	l := PostLike{}
	if got := l.TableName(); got != "blog_post_likes" {
		t.Errorf("expected table name %q, got %q", "blog_post_likes", got)
	}
}

func TestPostLikeBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		l := &PostLike{}
		if err := l.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if l.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		l := &PostLike{ID: existing}
		if err := l.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if l.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, l.ID)
		}
	})
}

// ---------------------------------------------------------------------------
// PostView
// ---------------------------------------------------------------------------

func TestPostViewTableName(t *testing.T) {
	v := PostView{}
	if got := v.TableName(); got != "blog_post_views" {
		t.Errorf("expected table name %q, got %q", "blog_post_views", got)
	}
}

func TestPostViewBeforeCreate(t *testing.T) {
	t.Run("generates UUID and sets ViewedAt when both are zero", func(t *testing.T) {
		v := &PostView{}
		if err := v.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if v.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
		if v.ViewedAt.IsZero() {
			t.Errorf("expected ViewedAt to be set, still zero")
		}
	})

	t.Run("preserves existing UUID and ViewedAt", func(t *testing.T) {
		existing := uuid.New()
		viewedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		v := &PostView{ID: existing, ViewedAt: viewedAt}
		if err := v.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if v.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, v.ID)
		}
		if !v.ViewedAt.Equal(viewedAt) {
			t.Errorf("expected ViewedAt %v to be preserved, got %v", viewedAt, v.ViewedAt)
		}
	})

	t.Run("generates UUID but preserves existing ViewedAt", func(t *testing.T) {
		viewedAt := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		v := &PostView{ViewedAt: viewedAt}
		if err := v.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if v.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
		if !v.ViewedAt.Equal(viewedAt) {
			t.Errorf("expected ViewedAt %v to be preserved, got %v", viewedAt, v.ViewedAt)
		}
	})
}

// ---------------------------------------------------------------------------
// PostShare
// ---------------------------------------------------------------------------

func TestPostShareTableName(t *testing.T) {
	s := PostShare{}
	if got := s.TableName(); got != "blog_post_shares" {
		t.Errorf("expected table name %q, got %q", "blog_post_shares", got)
	}
}

func TestPostShareBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		s := &PostShare{}
		if err := s.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if s.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		s := &PostShare{ID: existing}
		if err := s.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if s.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, s.ID)
		}
	})
}

// ---------------------------------------------------------------------------
// PostMedia
// ---------------------------------------------------------------------------

func TestPostMediaTableName(t *testing.T) {
	m := PostMedia{}
	if got := m.TableName(); got != "blog_post_media" {
		t.Errorf("expected table name %q, got %q", "blog_post_media", got)
	}
}

func TestPostMediaBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		m := &PostMedia{}
		if err := m.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if m.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		m := &PostMedia{ID: existing}
		if err := m.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if m.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, m.ID)
		}
	})
}

func TestMediaTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		mt       MediaType
		expected string
	}{
		{"image type", MediaTypeImage, "image"},
		{"video type", MediaTypeVideo, "video"},
		{"file type", MediaTypeFile, "file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mt) != tt.expected {
				t.Errorf("MediaType = %q, want %q", tt.mt, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PostRevision
// ---------------------------------------------------------------------------

func TestPostRevisionTableName(t *testing.T) {
	r := PostRevision{}
	if got := r.TableName(); got != "blog_post_revisions" {
		t.Errorf("expected table name %q, got %q", "blog_post_revisions", got)
	}
}

func TestPostRevisionBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		r := &PostRevision{}
		if err := r.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if r.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		r := &PostRevision{ID: existing}
		if err := r.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if r.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, r.ID)
		}
	})
}

func TestPostRevisionVersionTracking(t *testing.T) {
	postID := uuid.New()
	editorID := uuid.New()

	revisions := []PostRevision{
		{PostID: postID, EditorID: editorID, Title: "First draft", Version: 1},
		{PostID: postID, EditorID: editorID, Title: "Second draft", Version: 2},
		{PostID: postID, EditorID: editorID, Title: "Third draft", Version: 3},
	}

	for i, r := range revisions {
		expectedVersion := i + 1
		if r.Version != expectedVersion {
			t.Errorf("revision[%d].Version = %d, want %d", i, r.Version, expectedVersion)
		}
		if r.PostID != postID {
			t.Errorf("revision[%d].PostID = %v, want %v", i, r.PostID, postID)
		}
	}
}

// ---------------------------------------------------------------------------
// BlogSettings
// ---------------------------------------------------------------------------

func TestBlogSettingsTableName(t *testing.T) {
	s := BlogSettings{}
	if got := s.TableName(); got != "blog_settings" {
		t.Errorf("expected table name %q, got %q", "blog_settings", got)
	}
}

func TestBlogSettingsBeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is nil", func(t *testing.T) {
		s := &BlogSettings{}
		if err := s.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if s.ID == uuid.Nil {
			t.Errorf("expected valid UUID after BeforeCreate, still nil")
		}
	})

	t.Run("preserves existing UUID", func(t *testing.T) {
		existing := uuid.New()
		s := &BlogSettings{ID: existing}
		if err := s.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate returned error: %v", err)
		}
		if s.ID != existing {
			t.Errorf("expected ID %v to be preserved, got %v", existing, s.ID)
		}
	})
}

func TestBlogSettingsToResponse(t *testing.T) {
	s := &BlogSettings{
		AutoApproveComments: true,
		PostsPerPage:        25,
		ViewCooldownMinutes: 15,
		FeedItemLimit:       100,
		ReadTimeWPM:         250,
	}

	resp := s.ToResponse()

	if resp.AutoApproveComments != true {
		t.Errorf("AutoApproveComments = %v, want true", resp.AutoApproveComments)
	}
	if resp.PostsPerPage != 25 {
		t.Errorf("PostsPerPage = %d, want 25", resp.PostsPerPage)
	}
	if resp.ViewCooldownMinutes != 15 {
		t.Errorf("ViewCooldownMinutes = %d, want 15", resp.ViewCooldownMinutes)
	}
	if resp.FeedItemLimit != 100 {
		t.Errorf("FeedItemLimit = %d, want 100", resp.FeedItemLimit)
	}
	if resp.ReadTimeWPM != 250 {
		t.Errorf("ReadTimeWPM = %d, want 250", resp.ReadTimeWPM)
	}
}

// ---------------------------------------------------------------------------
// PostTag
// ---------------------------------------------------------------------------

func TestPostTagTableName(t *testing.T) {
	pt := PostTag{}
	if got := pt.TableName(); got != "post_tags" {
		t.Errorf("expected table name %q, got %q", "post_tags", got)
	}
}

// ---------------------------------------------------------------------------
// Comment ToResponse / ToAdminResponse
// ---------------------------------------------------------------------------

func TestCommentToResponse(t *testing.T) {
	t.Run("basic comment without children", func(t *testing.T) {
		commentID := uuid.New()
		postID := uuid.New()
		c := &Comment{
			ID:        commentID,
			PostID:    postID,
			Content:   "Test comment",
			GuestName: "Alice",
			Status:    CommentStatusApproved,
		}

		resp := c.ToResponse()

		if resp.ID != commentID {
			t.Errorf("ID = %v, want %v", resp.ID, commentID)
		}
		if resp.PostID != postID {
			t.Errorf("PostID = %v, want %v", resp.PostID, postID)
		}
		if resp.Content != "Test comment" {
			t.Errorf("Content = %q, want %q", resp.Content, "Test comment")
		}
		if resp.GuestName != "Alice" {
			t.Errorf("GuestName = %q, want %q", resp.GuestName, "Alice")
		}
		if resp.Status != CommentStatusApproved {
			t.Errorf("Status = %q, want %q", resp.Status, CommentStatusApproved)
		}
		if resp.Children != nil {
			t.Errorf("Children should be nil for comment without children, got %v", resp.Children)
		}
	})

	t.Run("comment with nested children", func(t *testing.T) {
		parentID := uuid.New()
		childID := uuid.New()
		postID := uuid.New()

		parent := &Comment{
			ID:      parentID,
			PostID:  postID,
			Content: "Parent comment",
			Status:  CommentStatusApproved,
			Children: []Comment{
				{
					ID:       childID,
					PostID:   postID,
					ParentID: &parentID,
					Content:  "Child reply",
					Status:   CommentStatusApproved,
				},
			},
		}

		resp := parent.ToResponse()

		if len(resp.Children) != 1 {
			t.Fatalf("expected 1 child, got %d", len(resp.Children))
		}
		if resp.Children[0].ID != childID {
			t.Errorf("child ID = %v, want %v", resp.Children[0].ID, childID)
		}
		if resp.Children[0].Content != "Child reply" {
			t.Errorf("child Content = %q, want %q", resp.Children[0].Content, "Child reply")
		}
	})

	t.Run("children are truncated at MaxCommentDepth", func(t *testing.T) {
		postID := uuid.New()

		// Build a chain deeper than MaxCommentDepth
		deepChild := Comment{
			ID:      uuid.New(),
			PostID:  postID,
			Content: "Very deep child",
			Status:  CommentStatusApproved,
		}

		// Build chain from depth MaxCommentDepth back to 0
		current := deepChild
		for i := MaxCommentDepth; i > 0; i-- {
			parentID := uuid.New()
			parent := Comment{
				ID:       parentID,
				PostID:   postID,
				Content:  "Parent",
				Status:   CommentStatusApproved,
				Children: []Comment{current},
			}
			current = parent
		}

		resp := current.toResponseWithDepth(0)

		// Walk the response tree to verify depth limit
		depth := 0
		node := resp
		for len(node.Children) > 0 {
			depth++
			node = &node.Children[0]
		}
		if depth > MaxCommentDepth {
			t.Errorf("response tree depth = %d, should not exceed MaxCommentDepth = %d", depth, MaxCommentDepth)
		}
	})

	t.Run("guest comment fields are preserved", func(t *testing.T) {
		c := &Comment{
			ID:         uuid.New(),
			PostID:     uuid.New(),
			Content:    "Guest says hi",
			GuestName:  "Bob",
			GuestEmail: "bob@example.com",
			Status:     CommentStatusPending,
		}

		resp := c.ToResponse()

		if resp.GuestName != "Bob" {
			t.Errorf("GuestName = %q, want %q", resp.GuestName, "Bob")
		}
		// ToResponse should not include GuestEmail (it's not in CommentResponse)
		// This is by design - CommentResponse struct does not have GuestEmail field
	})

	t.Run("authenticated comment preserves AuthorID", func(t *testing.T) {
		authorID := uuid.New()
		c := &Comment{
			ID:       uuid.New(),
			PostID:   uuid.New(),
			AuthorID: &authorID,
			Content:  "Auth user comment",
			Status:   CommentStatusApproved,
		}

		resp := c.ToResponse()

		if resp.AuthorID == nil || *resp.AuthorID != authorID {
			t.Errorf("AuthorID = %v, want %v", resp.AuthorID, authorID)
		}
	})
}

func TestCommentToAdminResponse(t *testing.T) {
	authorID := uuid.New()
	parentID := uuid.New()
	commentID := uuid.New()
	postID := uuid.New()
	now := time.Now()

	c := &Comment{
		ID:         commentID,
		PostID:     postID,
		AuthorID:   &authorID,
		ParentID:   &parentID,
		Content:    "Moderated comment",
		GuestName:  "Eve",
		GuestEmail: "eve@example.com",
		Status:     CommentStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	resp := c.ToAdminResponse()

	if resp.ID != commentID {
		t.Errorf("ID = %v, want %v", resp.ID, commentID)
	}
	if resp.PostID != postID {
		t.Errorf("PostID = %v, want %v", resp.PostID, postID)
	}
	if resp.AuthorID == nil || *resp.AuthorID != authorID {
		t.Errorf("AuthorID = %v, want %v", resp.AuthorID, &authorID)
	}
	if resp.ParentID == nil || *resp.ParentID != parentID {
		t.Errorf("ParentID = %v, want %v", resp.ParentID, &parentID)
	}
	if resp.Content != "Moderated comment" {
		t.Errorf("Content = %q, want %q", resp.Content, "Moderated comment")
	}
	if resp.GuestName != "Eve" {
		t.Errorf("GuestName = %q, want %q", resp.GuestName, "Eve")
	}
	if resp.GuestEmail != "eve@example.com" {
		t.Errorf("GuestEmail = %q, want %q", resp.GuestEmail, "eve@example.com")
	}
	if resp.Status != CommentStatusPending {
		t.Errorf("Status = %q, want %q", resp.Status, CommentStatusPending)
	}
}

// ---------------------------------------------------------------------------
// Comment status constants
// ---------------------------------------------------------------------------

func TestCommentStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		status   CommentStatus
		expected string
	}{
		{"pending", CommentStatusPending, "pending"},
		{"approved", CommentStatusApproved, "approved"},
		{"rejected", CommentStatusRejected, "rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("CommentStatus = %q, want %q", tt.status, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Category hierarchy
// ---------------------------------------------------------------------------

func TestCategoryHierarchy(t *testing.T) {
	t.Run("parent with children", func(t *testing.T) {
		parent := &Category{
			ID:   uuid.New(),
			Name: "Tech",
			Slug: "tech",
			Children: []Category{
				{ID: uuid.New(), Name: "Go", Slug: "go"},
				{ID: uuid.New(), Name: "Rust", Slug: "rust"},
			},
		}

		if !parent.IsRoot() {
			t.Errorf("parent should be root")
		}
		if len(parent.Children) != 2 {
			t.Errorf("expected 2 children, got %d", len(parent.Children))
		}
	})

	t.Run("child referencing parent", func(t *testing.T) {
		parentID := uuid.New()
		child := &Category{
			ID:       uuid.New(),
			Name:     "Go",
			Slug:     "go",
			ParentID: &parentID,
		}

		if child.IsRoot() {
			t.Errorf("child with ParentID should not be root")
		}
	})
}

// ---------------------------------------------------------------------------
// SSE Domain Events
// ---------------------------------------------------------------------------

func TestNewSSEBlogPostEvent(t *testing.T) {
	data := SSEBlogPostData{
		PostID:     uuid.New(),
		Title:      "Test Post",
		Slug:       "test-post",
		AuthorID:   uuid.New(),
		AuthorName: "Author",
	}

	event := NewSSEBlogPostEvent(SSEEventTypeBlogPostPublished, data)

	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.ID == "" {
		t.Errorf("expected non-empty event ID")
	}
	if event.Type != SSEEventTypeBlogPostPublished {
		t.Errorf("Type = %q, want %q", event.Type, SSEEventTypeBlogPostPublished)
	}
	if event.Timestamp.IsZero() {
		t.Errorf("expected non-zero Timestamp")
	}
	eventData, ok := event.Data.(SSEBlogPostData)
	if !ok {
		t.Fatalf("Data is %T, want SSEBlogPostData", event.Data)
	}
	if eventData.Title != "Test Post" {
		t.Errorf("Data.Title = %q, want %q", eventData.Title, "Test Post")
	}
}

func TestNewSSEBlogPostEventTypes(t *testing.T) {
	data := SSEBlogPostData{PostID: uuid.New(), Title: "Post"}

	tests := []struct {
		name      string
		eventType notificationDomain.SSEEventType
	}{
		{"published", SSEEventTypeBlogPostPublished},
		{"updated", SSEEventTypeBlogPostUpdated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewSSEBlogPostEvent(tt.eventType, data)
			if event.Type != tt.eventType {
				t.Errorf("Type = %q, want %q", event.Type, tt.eventType)
			}
		})
	}
}

func TestNewSSEBlogCommentEvent(t *testing.T) {
	authorID := uuid.New()
	data := SSEBlogCommentData{
		CommentID:  uuid.New(),
		PostID:     uuid.New(),
		PostTitle:  "Test Post",
		AuthorID:   &authorID,
		AuthorName: "Commenter",
		Content:    "Nice post!",
	}

	event := NewSSEBlogCommentEvent(SSEEventTypeBlogCommentNew, data)

	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != SSEEventTypeBlogCommentNew {
		t.Errorf("Type = %q, want %q", event.Type, SSEEventTypeBlogCommentNew)
	}
	eventData, ok := event.Data.(SSEBlogCommentData)
	if !ok {
		t.Fatalf("Data is %T, want SSEBlogCommentData", event.Data)
	}
	if eventData.Content != "Nice post!" {
		t.Errorf("Data.Content = %q, want %q", eventData.Content, "Nice post!")
	}
}

func TestNewSSEBlogLikeEvent(t *testing.T) {
	data := SSEBlogLikeData{
		PostID:    uuid.New(),
		UserID:    uuid.New(),
		LikeCount: 42,
		Liked:     true,
	}

	event := NewSSEBlogLikeEvent(data)

	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != SSEEventTypeBlogPostLiked {
		t.Errorf("Type = %q, want %q", event.Type, SSEEventTypeBlogPostLiked)
	}
	eventData, ok := event.Data.(SSEBlogLikeData)
	if !ok {
		t.Fatalf("Data is %T, want SSEBlogLikeData", event.Data)
	}
	if eventData.LikeCount != 42 {
		t.Errorf("Data.LikeCount = %d, want 42", eventData.LikeCount)
	}
	if !eventData.Liked {
		t.Errorf("Data.Liked = false, want true")
	}
}

func TestNewSSEBlogEngagementEvent(t *testing.T) {
	data := SSEBlogEngagementData{
		PostID:       uuid.New(),
		LikeCount:    10,
		ViewCount:    500,
		ShareCount:   5,
		CommentCount: 3,
	}

	event := NewSSEBlogEngagementEvent(data)

	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != SSEEventTypeBlogPostEngagement {
		t.Errorf("Type = %q, want %q", event.Type, SSEEventTypeBlogPostEngagement)
	}
	eventData, ok := event.Data.(SSEBlogEngagementData)
	if !ok {
		t.Fatalf("Data is %T, want SSEBlogEngagementData", event.Data)
	}
	if eventData.ViewCount != 500 {
		t.Errorf("Data.ViewCount = %d, want 500", eventData.ViewCount)
	}
}

func TestSSEBlogEventDataMarshaling(t *testing.T) {
	t.Run("SSEBlogPostData marshals to valid JSON", func(t *testing.T) {
		now := time.Now()
		data := SSEBlogPostData{
			PostID:      uuid.New(),
			Title:       "Test Post",
			Slug:        "test-post",
			AuthorID:    uuid.New(),
			AuthorName:  "Author",
			PublishedAt: &now,
		}

		b, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded SSEBlogPostData
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		if decoded.Title != data.Title {
			t.Errorf("Title = %q, want %q", decoded.Title, data.Title)
		}
		if decoded.Slug != data.Slug {
			t.Errorf("Slug = %q, want %q", decoded.Slug, data.Slug)
		}
	})

	t.Run("SSEBlogCommentData marshals to valid JSON", func(t *testing.T) {
		data := SSEBlogCommentData{
			CommentID:  uuid.New(),
			PostID:     uuid.New(),
			PostTitle:  "Test",
			AuthorName: "Guest",
			Content:    "Hello",
		}

		b, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded SSEBlogCommentData
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		if decoded.AuthorName != "Guest" {
			t.Errorf("AuthorName = %q, want %q", decoded.AuthorName, "Guest")
		}
	})

	t.Run("SSEBlogLikeData marshals to valid JSON", func(t *testing.T) {
		data := SSEBlogLikeData{
			PostID:    uuid.New(),
			UserID:    uuid.New(),
			LikeCount: 10,
			Liked:     true,
		}

		b, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded SSEBlogLikeData
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		if decoded.LikeCount != 10 {
			t.Errorf("LikeCount = %d, want 10", decoded.LikeCount)
		}
	})

	t.Run("SSEBlogEngagementData marshals to valid JSON", func(t *testing.T) {
		data := SSEBlogEngagementData{
			PostID:       uuid.New(),
			LikeCount:    5,
			ViewCount:    100,
			ShareCount:   2,
			CommentCount: 8,
		}

		b, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded SSEBlogEngagementData
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		if decoded.ShareCount != 2 {
			t.Errorf("ShareCount = %d, want 2", decoded.ShareCount)
		}
	})
}

// ---------------------------------------------------------------------------
// SSE Event type constants
// ---------------------------------------------------------------------------

func TestSSEEventTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		et       notificationDomain.SSEEventType
		expected string
	}{
		{"post published", SSEEventTypeBlogPostPublished, "blog:post:published"},
		{"post updated", SSEEventTypeBlogPostUpdated, "blog:post:updated"},
		{"comment new", SSEEventTypeBlogCommentNew, "blog:comment:new"},
		{"comment approved", SSEEventTypeBlogCommentApproved, "blog:comment:approved"},
		{"post liked", SSEEventTypeBlogPostLiked, "blog:post:liked"},
		{"post engagement", SSEEventTypeBlogPostEngagement, "blog:post:engagement"},
		{"draft autosave", SSEEventTypeBlogDraftAutosave, "blog:draft:autosave"},
		{"admin stats", SSEEventTypeBlogAdminStats, "blog:admin:stats"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.et) != tt.expected {
				t.Errorf("SSEEventType = %q, want %q", tt.et, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TrendingPost
// ---------------------------------------------------------------------------

func TestTrendingPostEmbedsPost(t *testing.T) {
	tp := TrendingPost{
		Post:          Post{ID: uuid.New(), Title: "Trending", Status: PostStatusPublished},
		TrendingScore: 99.5,
	}

	if tp.Title != "Trending" {
		t.Errorf("Title = %q, want %q", tp.Title, "Trending")
	}
	if tp.TrendingScore != 99.5 {
		t.Errorf("TrendingScore = %f, want 99.5", tp.TrendingScore)
	}
	if !tp.IsPublished() {
		t.Errorf("expected embedded Post methods to work")
	}
}
