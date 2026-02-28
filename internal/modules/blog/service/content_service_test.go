package service

import (
	"strings"
	"testing"
)

func TestContentService_SerializeToHTML_Paragraph(t *testing.T) {
	svc := NewContentService()

	input := []byte(`[{"type":"p","children":[{"text":"Hello world"}]}]`)
	got, err := svc.SerializeToHTML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "<p>Hello world</p>" {
		t.Errorf("got %q, want %q", got, "<p>Hello world</p>")
	}
}

func TestContentService_SerializeToHTML_Heading(t *testing.T) {
	svc := NewContentService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "h1 node type",
			input: `[{"type":"h1","children":[{"text":"Title"}]}]`,
			want:  "<h1>Title</h1>",
		},
		{
			name:  "heading-one node type",
			input: `[{"type":"heading-one","children":[{"text":"Title"}]}]`,
			want:  "<h1>Title</h1>",
		},
		{
			name:  "h2 node type",
			input: `[{"type":"h2","children":[{"text":"Subtitle"}]}]`,
			want:  "<h2>Subtitle</h2>",
		},
		{
			name:  "heading-two node type",
			input: `[{"type":"heading-two","children":[{"text":"Subtitle"}]}]`,
			want:  "<h2>Subtitle</h2>",
		},
		{
			name:  "h3 node type",
			input: `[{"type":"h3","children":[{"text":"Section"}]}]`,
			want:  "<h3>Section</h3>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.SerializeToHTML([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentService_SerializeToHTML_Bold(t *testing.T) {
	svc := NewContentService()

	input := []byte(`[{"type":"p","children":[{"text":"bold text","bold":true}]}]`)
	got, err := svc.SerializeToHTML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<p><strong>bold text</strong></p>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContentService_SerializeToHTML_Italic(t *testing.T) {
	svc := NewContentService()

	input := []byte(`[{"type":"p","children":[{"text":"italic text","italic":true}]}]`)
	got, err := svc.SerializeToHTML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "<p><em>italic text</em></p>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContentService_SerializeToHTML_BoldAndItalic(t *testing.T) {
	svc := NewContentService()

	// bold + italic birlikte
	input := []byte(`[{"type":"p","children":[{"text":"both","bold":true,"italic":true}]}]`)
	got, err := svc.SerializeToHTML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// bold + italic sıralaması: önce strong, sonra em
	if !strings.Contains(got, "<strong>") || !strings.Contains(got, "<em>") {
		t.Errorf("expected both <strong> and <em> tags in output, got: %q", got)
	}
}

func TestContentService_SerializeToHTML_CodeBlock(t *testing.T) {
	svc := NewContentService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "code_block without language",
			input: `[{"type":"code_block","children":[{"text":"fmt.Println()"}]}]`,
			want:  `<pre><code>fmt.Println()</code></pre>`,
		},
		{
			name:  "code_block with language",
			input: `[{"type":"code_block","language":"go","children":[{"text":"fmt.Println()"}]}]`,
			want:  `<pre><code class="language-go">fmt.Println()</code></pre>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.SerializeToHTML([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentService_SerializeToHTML_Link(t *testing.T) {
	svc := NewContentService()

	input := []byte(`[{"type":"a","url":"https://example.com","children":[{"text":"click here"}]}]`)
	got, err := svc.SerializeToHTML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<a href="https://example.com" rel="noopener noreferrer">click here</a>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContentService_SerializeToHTML_List(t *testing.T) {
	svc := NewContentService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "bulleted list",
			input: `[{"type":"ul","children":[
				{"type":"li","children":[{"text":"item one"}]},
				{"type":"li","children":[{"text":"item two"}]}
			]}]`,
			want: "<ul><li>item one</li><li>item two</li></ul>",
		},
		{
			name: "numbered list",
			input: `[{"type":"ol","children":[
				{"type":"li","children":[{"text":"first"}]},
				{"type":"li","children":[{"text":"second"}]}
			]}]`,
			want: "<ol><li>first</li><li>second</li></ol>",
		},
		{
			name: "bulleted-list alias",
			input: `[{"type":"bulleted-list","children":[
				{"type":"list-item","children":[{"text":"alpha"}]}
			]}]`,
			want: "<ul><li>alpha</li></ul>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.SerializeToHTML([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentService_SerializeToHTML_Image(t *testing.T) {
	svc := NewContentService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "img node type",
			input: `[{"type":"img","src":"https://example.com/photo.jpg","alt":"A photo"}]`,
			want:  `<img src="https://example.com/photo.jpg" alt="A photo" />`,
		},
		{
			name:  "image node type",
			input: `[{"type":"image","src":"https://example.com/pic.png","alt":"A picture"}]`,
			want:  `<img src="https://example.com/pic.png" alt="A picture" />`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.SerializeToHTML([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentService_SerializeToHTML_InvalidJSON(t *testing.T) {
	svc := NewContentService()

	_, err := svc.SerializeToHTML([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestContentService_ExtractPlainText(t *testing.T) {
	svc := NewContentService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single paragraph",
			input: `[{"type":"p","children":[{"text":"Hello world"}]}]`,
			want:  "Hello world",
		},
		{
			name: "multiple paragraphs",
			input: `[
				{"type":"p","children":[{"text":"First paragraph"}]},
				{"type":"p","children":[{"text":"Second paragraph"}]}
			]`,
			want: "First paragraph\nSecond paragraph",
		},
		{
			name: "heading and paragraph",
			input: `[
				{"type":"h1","children":[{"text":"Title"}]},
				{"type":"p","children":[{"text":"Body text"}]}
			]`,
			want: "Title\nBody text",
		},
		{
			name:  "bold text extracted as plain",
			input: `[{"type":"p","children":[{"text":"bold","bold":true}]}]`,
			want:  "bold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ExtractPlainText([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentService_ExtractPlainText_InvalidJSON(t *testing.T) {
	svc := NewContentService()

	_, err := svc.ExtractPlainText([]byte(`{bad json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestContentService_SanitizeHTML_XSSRemoved(t *testing.T) {
	svc := NewContentService()

	dangerous := `<p>Hello</p><script>alert('xss')</script>`
	got := svc.SanitizeHTML(dangerous)

	if strings.Contains(got, "<script>") {
		t.Errorf("SanitizeHTML did not remove <script> tag, got: %q", got)
	}
	if strings.Contains(got, "alert") {
		t.Errorf("SanitizeHTML did not remove script content, got: %q", got)
	}
	if !strings.Contains(got, "<p>Hello</p>") {
		t.Errorf("SanitizeHTML removed legitimate content, got: %q", got)
	}
}

func TestContentService_SanitizeHTML_OnclickRemoved(t *testing.T) {
	svc := NewContentService()

	input := `<p onclick="evil()">Text</p>`
	got := svc.SanitizeHTML(input)

	if strings.Contains(got, "onclick") {
		t.Errorf("SanitizeHTML did not remove onclick attribute, got: %q", got)
	}
}

func TestContentService_SanitizeHTML_SafeTagsPreserved(t *testing.T) {
	svc := NewContentService()

	input := `<p>Normal <strong>bold</strong> and <em>italic</em> text</p>`
	got := svc.SanitizeHTML(input)

	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("SanitizeHTML removed <strong> tag, got: %q", got)
	}
	if !strings.Contains(got, "<em>italic</em>") {
		t.Errorf("SanitizeHTML removed <em> tag, got: %q", got)
	}
}

func TestContentService_ValidateContent_Empty(t *testing.T) {
	svc := NewContentService()

	err := svc.ValidateContent([]byte(`[]`))
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to contain 'empty', got: %v", err)
	}
}

func TestContentService_ValidateContent_TooLarge(t *testing.T) {
	svc := NewContentService()

	// 5MB + 1 byte'lık geçersiz içerik oluştur
	large := make([]byte, 5*1024*1024+1)
	for i := range large {
		large[i] = 'x'
	}

	err := svc.ValidateContent(large)
	if err == nil {
		t.Fatal("expected error for too large content, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected error message to contain 'too large', got: %v", err)
	}
}

func TestContentService_ValidateContent_InvalidJSON(t *testing.T) {
	svc := NewContentService()

	err := svc.ValidateContent([]byte(`not valid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected error message to contain 'invalid', got: %v", err)
	}
}

func TestContentService_ValidateContent_Valid(t *testing.T) {
	svc := NewContentService()

	input := []byte(`[{"type":"p","children":[{"text":"Hello"}]}]`)
	err := svc.ValidateContent(input)
	if err != nil {
		t.Errorf("unexpected error for valid content: %v", err)
	}
}
