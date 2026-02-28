package service

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// ContentService handles content serialization and sanitization
type ContentService struct {
	sanitizer *bluemonday.Policy
}

// NewContentService creates a new ContentService
func NewContentService() *ContentService {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("pre", "code", "span", "div")
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	p.AllowAttrs("href", "title", "target", "rel").OnElements("a")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	return &ContentService{sanitizer: p}
}

// SlateNode represents a node in a Plate.js/Slate JSON document
type SlateNode struct {
	Type     string      `json:"type,omitempty"`
	Text     string      `json:"text,omitempty"`
	Children []SlateNode `json:"children,omitempty"`
	URL      string      `json:"url,omitempty"`
	Src      string      `json:"src,omitempty"`
	Alt      string      `json:"alt,omitempty"`
	Language string      `json:"language,omitempty"`
	// Marks
	Bold          bool `json:"bold,omitempty"`
	Italic        bool `json:"italic,omitempty"`
	Underline     bool `json:"underline,omitempty"`
	Strikethrough bool `json:"strikethrough,omitempty"`
	Code          bool `json:"code,omitempty"`
}

// SerializeToHTML converts Plate.js JSON content to HTML
func (s *ContentService) SerializeToHTML(contentJSON []byte) (string, error) {
	var nodes []SlateNode
	if err := json.Unmarshal(contentJSON, &nodes); err != nil {
		return "", fmt.Errorf("invalid content JSON: %w", err)
	}

	var b strings.Builder
	for i := range nodes {
		s.renderNode(&b, &nodes[i])
	}
	return b.String(), nil
}

// ExtractPlainText converts Plate.js JSON content to plain text
func (s *ContentService) ExtractPlainText(contentJSON []byte) (string, error) {
	var nodes []SlateNode
	if err := json.Unmarshal(contentJSON, &nodes); err != nil {
		return "", fmt.Errorf("invalid content JSON: %w", err)
	}

	var b strings.Builder
	for i := range nodes {
		s.extractText(&b, &nodes[i])
	}
	return strings.TrimSpace(b.String()), nil
}

// SanitizeHTML sanitizes HTML content using bluemonday
func (s *ContentService) SanitizeHTML(html string) string {
	return s.sanitizer.Sanitize(html)
}

// ValidateContent validates content JSON structure
func (s *ContentService) ValidateContent(contentJSON []byte) error {
	if len(contentJSON) > 5*1024*1024 { // 5MB max
		return fmt.Errorf("content too large: %d bytes", len(contentJSON))
	}

	var nodes []SlateNode
	if err := json.Unmarshal(contentJSON, &nodes); err != nil {
		return fmt.Errorf("invalid content JSON: %w", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("content cannot be empty")
	}

	return nil
}

// nodeTagMap maps Slate node types to their HTML tag names for simple wrap elements.
var nodeTagMap = map[string]string{
	"p": "p", "paragraph": "p",
	"h1": "h1", "heading-one": "h1",
	"h2": "h2", "heading-two": "h2",
	"h3": "h3", "heading-three": "h3",
	"h4": "h4", "h5": "h5", "h6": "h6",
	"blockquote": "blockquote",
	"ul":         "ul", "bulleted-list": "ul",
	"ol": "ol", "numbered-list": "ol",
	"li": "li", "list-item": "li",
	"table": "table",
	"tr":    "tr", "table-row": "tr",
	"td": "td", "table-cell": "td",
}

func (s *ContentService) renderNode(b *strings.Builder, node *SlateNode) {
	// Text leaf node
	if node.Type == "" && node.Text != "" {
		s.renderTextLeaf(b, node)
		return
	}

	// Simple wrap elements (tag lookup)
	if tag, ok := nodeTagMap[node.Type]; ok {
		b.WriteString("<" + tag + ">")
		s.renderChildren(b, node)
		b.WriteString("</" + tag + ">")
		return
	}

	// Special elements
	switch node.Type {
	case "code_block":
		lang := ""
		if node.Language != "" {
			lang = fmt.Sprintf(` class="language-%s"`,
				html.EscapeString(node.Language))
		}
		fmt.Fprintf(b, "<pre><code%s>", lang)
		s.renderChildren(b, node)
		b.WriteString("</code></pre>")
	case "img", "image":
		fmt.Fprintf(b, `<img src="%s" alt="%s" />`,
			html.EscapeString(node.Src),
			html.EscapeString(node.Alt))
	case "a", "link":
		fmt.Fprintf(b, `<a href="%s" rel="noopener noreferrer">`,
			html.EscapeString(node.URL))
		s.renderChildren(b, node)
		b.WriteString("</a>")
	case "hr":
		b.WriteString("<hr />")
	default:
		s.renderChildren(b, node)
	}
}

func (s *ContentService) renderTextLeaf(b *strings.Builder, node *SlateNode) {
	text := html.EscapeString(node.Text)
	if node.Code {
		b.WriteString("<code>")
		b.WriteString(text)
		b.WriteString("</code>")
		return
	}
	if node.Bold {
		text = "<strong>" + text + "</strong>"
	}
	if node.Italic {
		text = "<em>" + text + "</em>"
	}
	if node.Underline {
		text = "<u>" + text + "</u>"
	}
	if node.Strikethrough {
		text = "<s>" + text + "</s>"
	}
	b.WriteString(text)
}

func (s *ContentService) renderChildren(b *strings.Builder, node *SlateNode) {
	for i := range node.Children {
		s.renderNode(b, &node.Children[i])
	}
}

func (s *ContentService) extractText(b *strings.Builder, node *SlateNode) {
	if node.Text != "" {
		b.WriteString(node.Text)
		return
	}

	for i := range node.Children {
		s.extractText(b, &node.Children[i])
	}

	// Add newline after block-level elements
	switch node.Type {
	case "p", "paragraph", "h1", "h2", "h3", "h4", "h5", "h6",
		"heading-one", "heading-two", "heading-three",
		"blockquote", "code_block", "li", "list-item", "hr":
		b.WriteString("\n")
	}
}
