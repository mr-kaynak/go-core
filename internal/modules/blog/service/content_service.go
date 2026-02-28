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
	for _, node := range nodes {
		s.renderNode(&b, node)
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
	for _, node := range nodes {
		s.extractText(&b, node)
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

func (s *ContentService) renderNode(b *strings.Builder, node SlateNode) {
	// Text leaf node
	if node.Type == "" && node.Text != "" {
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
		return
	}

	// Element nodes
	switch node.Type {
	case "p", "paragraph":
		b.WriteString("<p>")
		s.renderChildren(b, node)
		b.WriteString("</p>")
	case "h1", "heading-one":
		b.WriteString("<h1>")
		s.renderChildren(b, node)
		b.WriteString("</h1>")
	case "h2", "heading-two":
		b.WriteString("<h2>")
		s.renderChildren(b, node)
		b.WriteString("</h2>")
	case "h3", "heading-three":
		b.WriteString("<h3>")
		s.renderChildren(b, node)
		b.WriteString("</h3>")
	case "h4":
		b.WriteString("<h4>")
		s.renderChildren(b, node)
		b.WriteString("</h4>")
	case "h5":
		b.WriteString("<h5>")
		s.renderChildren(b, node)
		b.WriteString("</h5>")
	case "h6":
		b.WriteString("<h6>")
		s.renderChildren(b, node)
		b.WriteString("</h6>")
	case "blockquote":
		b.WriteString("<blockquote>")
		s.renderChildren(b, node)
		b.WriteString("</blockquote>")
	case "code_block":
		lang := ""
		if node.Language != "" {
			lang = fmt.Sprintf(` class="language-%s"`, html.EscapeString(node.Language))
		}
		b.WriteString(fmt.Sprintf("<pre><code%s>", lang))
		s.renderChildren(b, node)
		b.WriteString("</code></pre>")
	case "img", "image":
		b.WriteString(fmt.Sprintf(`<img src="%s" alt="%s" />`, html.EscapeString(node.Src), html.EscapeString(node.Alt)))
	case "a", "link":
		b.WriteString(fmt.Sprintf(`<a href="%s" rel="noopener noreferrer">`, html.EscapeString(node.URL)))
		s.renderChildren(b, node)
		b.WriteString("</a>")
	case "ul", "bulleted-list":
		b.WriteString("<ul>")
		s.renderChildren(b, node)
		b.WriteString("</ul>")
	case "ol", "numbered-list":
		b.WriteString("<ol>")
		s.renderChildren(b, node)
		b.WriteString("</ol>")
	case "li", "list-item":
		b.WriteString("<li>")
		s.renderChildren(b, node)
		b.WriteString("</li>")
	case "table":
		b.WriteString("<table>")
		s.renderChildren(b, node)
		b.WriteString("</table>")
	case "tr", "table-row":
		b.WriteString("<tr>")
		s.renderChildren(b, node)
		b.WriteString("</tr>")
	case "td", "table-cell":
		b.WriteString("<td>")
		s.renderChildren(b, node)
		b.WriteString("</td>")
	case "hr":
		b.WriteString("<hr />")
	default:
		// Unknown node type — render children
		s.renderChildren(b, node)
	}
}

func (s *ContentService) renderChildren(b *strings.Builder, node SlateNode) {
	for _, child := range node.Children {
		s.renderNode(b, child)
	}
}

func (s *ContentService) extractText(b *strings.Builder, node SlateNode) {
	if node.Text != "" {
		b.WriteString(node.Text)
		return
	}

	for _, child := range node.Children {
		s.extractText(b, child)
	}

	// Add newline after block-level elements
	switch node.Type {
	case "p", "paragraph", "h1", "h2", "h3", "h4", "h5", "h6",
		"heading-one", "heading-two", "heading-three",
		"blockquote", "code_block", "li", "list-item", "hr":
		b.WriteString("\n")
	}
}
