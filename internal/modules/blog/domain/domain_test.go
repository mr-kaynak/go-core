package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
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
