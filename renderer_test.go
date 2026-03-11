package prettier_test

import (
	"bytes"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	prettier "github.com/ajbeck/goldmark-prettier-markdown"
)

func newTestMarkdown(opts ...prettier.Option) goldmark.Markdown {
	r := prettier.NewRenderer(opts...)
	return goldmark.New(
		goldmark.WithRenderer(
			renderer.NewRenderer(
				renderer.WithNodeRenderers(
					util.Prioritized(r, 1000),
				),
			),
		),
	)
}

func newTestMarkdownGFM(opts ...prettier.Option) goldmark.Markdown {
	r := prettier.NewRenderer(opts...)
	md := goldmark.New(
		goldmark.WithRenderer(
			renderer.NewRenderer(
				renderer.WithNodeRenderers(
					util.Prioritized(r, 1000),
				),
			),
		),
	)
	// Add only the PARSER parts of GFM extensions — not their HTML renderers,
	// which would conflict with our markdown renderer.
	md.Parser().AddOptions(
		parser.WithParagraphTransformers(
			util.Prioritized(extension.NewTableParagraphTransformer(), 200),
		),
	)
	md.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(extension.NewStrikethroughParser(), 500),
		),
	)
	return md
}

func render(t *testing.T, md goldmark.Markdown, input string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := md.Convert([]byte(input), &buf); err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	return buf.String()
}

func TestATXHeadings(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "h1", input: "# Hello", want: "# Hello\n"},
		{name: "h2", input: "## World", want: "## World\n"},
		{name: "h3", input: "### Level 3", want: "### Level 3\n"},
		{name: "h6", input: "###### Deep", want: "###### Deep\n"},
		{name: "empty", input: "#", want: "#\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSetextHeadings(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "h1_setext",
			input: "Hello\n=====",
			want:  "Hello\n=====\n",
		},
		{
			name:  "h2_setext",
			input: "World\n-----",
			want:  "World\n-----\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Hello world.\n\nSecond paragraph."
	want := "Hello world.\n\nSecond paragraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestBlockquote(t *testing.T) {
	md := newTestMarkdown()
	input := "> Hello\n> World"
	want := "> Hello\n> World\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestFencedCodeBlock(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic",
			input: "```\ncode\n```",
			want:  "```\ncode\n```\n",
		},
		{
			name:  "with_lang",
			input: "```go\nfmt.Println()\n```",
			want:  "```go\nfmt.Println()\n```\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestThematicBreak(t *testing.T) {
	md := newTestMarkdown()
	input := "Above\n\n---\n\nBelow"
	want := "Above\n\n---\n\nBelow\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestEmphasisAndStrong(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "emphasis_default", input: "*hello*", want: "_hello_\n"},
		{name: "emphasis_underscore", input: "_hello_", want: "_hello_\n"},
		{name: "strong", input: "**hello**", want: "**hello**\n"},
		{name: "strong_underscore", input: "__hello__", want: "**hello**\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEmphasisMarkerSelection(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "adjacent_word_uses_asterisk",
			input: "a*b*c",
			want:  "a*b*c\n",
		},
		{
			name:  "space_separated_uses_underscore",
			input: "a *b* c",
			want:  "a _b_ c\n",
		},
		{
			name:  "punctuation_before_uses_underscore",
			input: "(*hello*)",
			want:  "(_hello_)\n",
		},
		{
			name:  "nested_emphasis_uses_asterisk",
			input: "_outer *inner* outer_",
			want:  "_outer *inner* outer_\n",
		},
		{
			// Goldmark parses a***b***c as: a + emphasis(strong(b)) + c
			// The outer emphasis uses '*' (adjacent words), inner strong is '**'.
			name:  "emphasis_wrapping_strong_adjacent",
			input: "a***b***c",
			want:  "a***b***c\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInlineCode(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple", input: "`code`", want: "`code`\n"},
		{name: "with_backtick", input: "`` ` ``", want: "`` ` ``\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLink(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic",
			input: "[text](http://example.com)",
			want:  "[text](http://example.com)\n",
		},
		{
			name:  "with_title",
			input: `[text](http://example.com "Title")`,
			want:  `[text](http://example.com "Title")` + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestImage(t *testing.T) {
	md := newTestMarkdown()
	input := "![alt](image.png)"
	want := "![alt](image.png)\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestAutoLink(t *testing.T) {
	md := newTestMarkdown()
	input := "<http://example.com>"
	want := "<http://example.com>\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestUnorderedList(t *testing.T) {
	md := newTestMarkdown()
	input := "- item 1\n- item 2\n- item 3"
	want := "- item 1\n- item 2\n- item 3\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestOrderedList(t *testing.T) {
	md := newTestMarkdown()
	input := "1. first\n2. second\n3. third"
	want := "1. first\n2. second\n3. third\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestHardLineBreak(t *testing.T) {
	md := newTestMarkdown()
	input := "line one\\\nline two"
	want := "line one\\\nline two\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestStrikethrough(t *testing.T) {
	md := newTestMarkdown()
	// Strikethrough requires GFM parser extension to parse, so test
	// that the renderer at least doesn't crash with basic markdown.
	input := "normal text"
	want := "normal text\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestInlineCodeEdgeCases(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "starts_with_backtick",
			input: "`` `foo ``",
			want:  "`` `foo ``\n",
		},
		{
			name:  "ends_with_backtick",
			input: "`` foo` ``",
			want:  "`` foo` ``\n",
		},
		{
			// Goldmark strips outer backtick pair and padding spaces from content.
			// Content becomes " code " — spaces remain, no backticks → single backtick.
			// Padding rule: starts & ends with space AND has non-space → add space pad.
			name:  "content_with_spaces",
			input: "``  code  ``",
			want:  "`  code  `\n",
		},
		{
			name:  "triple_backtick_content",
			input: "` `` `",
			want:  "` `` `\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestImageWithTitle(t *testing.T) {
	md := newTestMarkdown()
	input := `![alt text](image.png "My Image")`
	want := `![alt text](image.png "My Image")` + "\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestRawHTMLInline(t *testing.T) {
	md := newTestMarkdown()
	input := "Text with <em>html</em> inline."
	want := "Text with <em>html</em> inline.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestStrongAndEmphasisCombined(t *testing.T) {
	md := newTestMarkdown()
	input := "***bold and italic***"
	want := "_**bold and italic**_\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestLinkURLEncoding(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal_url",
			input: "[text](http://example.com)",
			want:  "[text](http://example.com)\n",
		},
		{
			name:  "url_with_parens",
			input: "[text](<http://example.com/foo(bar)>)",
			want:  "[text](<http://example.com/foo(bar)>)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSingleQuoteOption(t *testing.T) {
	md := newTestMarkdown(prettier.WithSingleQuote(true))
	input := `[text](http://example.com "Title")`
	want := `[text](http://example.com 'Title')` + "\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

// --- Phase 2: Block element tests ---

func TestBlockSpacing_HeadingAfterParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Paragraph.\n\n## Heading\n\nAnother."
	want := "Paragraph.\n\n## Heading\n\nAnother.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestBlockSpacing_CodeAfterParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Paragraph.\n\n```\ncode\n```\n\nAfter."
	want := "Paragraph.\n\n```\ncode\n```\n\nAfter.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestBlockSpacing_BlockquoteAfterParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Paragraph.\n\n> Quote.\n\nAfter."
	want := "Paragraph.\n\n> Quote.\n\nAfter.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestBlockSpacing_ThematicBreakBetweenParagraphs(t *testing.T) {
	md := newTestMarkdown()
	input := "Above.\n\n---\n\nBelow."
	want := "Above.\n\n---\n\nBelow.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestTightList(t *testing.T) {
	md := newTestMarkdown()
	input := "- one\n- two\n- three"
	want := "- one\n- two\n- three\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestLooseList(t *testing.T) {
	md := newTestMarkdown()
	input := "- one\n\n- two\n\n- three"
	want := "- one\n\n- two\n\n- three\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestNestedBlockquote(t *testing.T) {
	md := newTestMarkdown()
	input := "> outer\n>\n> > inner"
	want := "> outer\n>\n> > inner\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestBlockquoteMultipleParagraphs(t *testing.T) {
	md := newTestMarkdown()
	input := "> First paragraph.\n>\n> Second paragraph."
	want := "> First paragraph.\n>\n> Second paragraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestNestedList(t *testing.T) {
	md := newTestMarkdown()
	input := "- outer 1\n  - inner 1\n  - inner 2\n- outer 2"
	want := "- outer 1\n  - inner 1\n  - inner 2\n- outer 2\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListWithParagraphs(t *testing.T) {
	md := newTestMarkdown()
	// Loose list: items separated by blank lines get wrapped in paragraphs.
	input := "- item one\n\n- item two"
	want := "- item one\n\n- item two\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFencedCodeBlockWithBackticks(t *testing.T) {
	md := newTestMarkdown()
	input := "````\n```\ncode\n```\n````"
	want := "````\n```\ncode\n```\n````\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestIndentedCodeBlock(t *testing.T) {
	md := newTestMarkdown()
	input := "    code line 1\n    code line 2"
	want := "    code line 1\n    code line 2\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestIndentedCodeBlockAfterList(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			// Empty list item followed by indented code: goldmark parses
			// the CodeBlock as a sibling of the List, not inside it.
			name:  "empty_list_item_then_code",
			input: "-\n\n    code",
			want:  "-\n\n\n    code\n",
		},
		{
			name:  "fenced_code_after_list_no_extra_blank",
			input: "- item\n\n```\ncode\n```",
			want:  "- item\n\n```\ncode\n```\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("render(%q) =\n%s\nwant:\n%s", tt.input, got, tt.want)
			}
		})
	}
}

func TestHTMLBlock(t *testing.T) {
	md := newTestMarkdown()
	input := "<div>\nhello\n</div>"
	want := "<div>\nhello\n</div>\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMultipleBlockTypes(t *testing.T) {
	md := newTestMarkdown()
	input := "# Title\n\nParagraph.\n\n> Quote.\n\n```\ncode\n```\n\n---\n\nEnd."
	want := "# Title\n\nParagraph.\n\n> Quote.\n\n```\ncode\n```\n\n---\n\nEnd.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListAfterParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Paragraph.\n\n- item 1\n- item 2"
	want := "Paragraph.\n\n- item 1\n- item 2\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOrderedListStartNumber(t *testing.T) {
	md := newTestMarkdown()
	input := "3. third\n4. fourth\n5. fifth"
	want := "3. third\n4. fourth\n5. fifth\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmptyDocument(t *testing.T) {
	md := newTestMarkdown()
	got := render(t, md, "")
	if got != "" {
		t.Errorf("Render empty doc = %q, want %q", got, "")
	}
}

func TestSoftLineBreak(t *testing.T) {
	md := newTestMarkdown()
	input := "line one\nline two"
	want := "line one\nline two\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestSetextHeadingAfterParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "Paragraph.\n\nSetext\n======"
	want := "Paragraph.\n\nSetext\n======\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteWithHeading(t *testing.T) {
	md := newTestMarkdown()
	input := "> # Heading\n>\n> Paragraph."
	want := "> # Heading\n>\n> Paragraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteWithList(t *testing.T) {
	md := newTestMarkdown()
	input := "> - item 1\n> - item 2"
	want := "> - item 1\n> - item 2\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListItemMultiLine(t *testing.T) {
	md := newTestMarkdown()
	input := "- first line\n  second line\n- next item"
	want := "- first line\n  second line\n- next item\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFencedCodeBlockMeta(t *testing.T) {
	md := newTestMarkdown()
	input := "```js title=\"example.js\"\nconsole.log(1)\n```"
	want := "```js title=\"example.js\"\nconsole.log(1)\n```\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFencedCodeBlockEmpty(t *testing.T) {
	md := newTestMarkdown()
	input := "```\n```"
	want := "```\n```\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteEmpty(t *testing.T) {
	md := newTestMarkdown()
	// An empty blockquote line between content produces a blank line inside.
	input := "> First.\n>\n> Second."
	want := "> First.\n>\n> Second.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListWithCodeBlock(t *testing.T) {
	md := newTestMarkdown()
	input := "- item 1\n\n  ```\n  code\n  ```\n\n- item 2"
	want := "- item 1\n\n  ```\n  code\n  ```\n\n- item 2\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetextInBlockquote(t *testing.T) {
	md := newTestMarkdown()
	input := "> Setext\n> ======"
	want := "> Setext\n> ======\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMultipleHeadings(t *testing.T) {
	md := newTestMarkdown()
	input := "# First\n\n## Second\n\n### Third"
	want := "# First\n\n## Second\n\n### Third\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHTMLComment(t *testing.T) {
	md := newTestMarkdown()
	input := "<!-- comment -->\n\nParagraph."
	want := "<!-- comment -->\n\nParagraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListNestedThreeLevels(t *testing.T) {
	md := newTestMarkdown()
	input := "- a\n  - b\n    - c"
	want := "- a\n  - b\n    - c\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteConsecutiveParagraphs(t *testing.T) {
	// Goldmark sets HasBlankPreviousLines=false for paragraphs inside
	// blockquotes, so we need special handling to insert blank lines.
	md := newTestMarkdown()
	input := "> Para one.\n>\n> Para two.\n>\n> Para three."
	want := "> Para one.\n>\n> Para two.\n>\n> Para three.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLooseListMultiParagraph(t *testing.T) {
	// A loose list item can contain multiple paragraphs.
	md := newTestMarkdown()
	input := "- Para one.\n\n  Para two.\n\n- Item two."
	want := "- Para one.\n\n  Para two.\n\n- Item two.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCodeBlockAfterParagraphInList(t *testing.T) {
	md := newTestMarkdown()
	input := "- paragraph\n\n  ```\n  code\n  ```"
	want := "- paragraph\n\n  ```\n  code\n  ```\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// --- Phase 5: Table tests ---

func TestBasicTable(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| a | b |\n| --- | --- |\n| c | d |"
	want := "| a   | b   |\n| --- | --- |\n| c   | d   |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableColumnAlignment(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Left | Center | Right | None |\n| :--- | :----: | ----: | ---- |\n| a | b | c | d |\n| longer | text | here | ok |"
	want := "| Left   | Center | Right | None |\n| :----- | :----: | ----: | ---- |\n| a      |   b    |     c | d    |\n| longer |  text  |  here | ok   |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableWithEmphasis(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Header |\n| --- |\n| **bold** |"
	want := "| Header   |\n| -------- |\n| **bold** |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableMinColumnWidth(t *testing.T) {
	md := newTestMarkdownGFM()
	// Single char columns padded to minimum width 3.
	input := "| a | b |\n| - | - |\n| c | d |"
	want := "| a   | b   |\n| --- | --- |\n| c   | d   |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableAfterParagraph(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "Paragraph.\n\n| a | b |\n| --- | --- |\n| c | d |"
	want := "Paragraph.\n\n| a   | b   |\n| --- | --- |\n| c   | d   |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableWithInlineCode(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Header |\n| --- |\n| `code` |"
	want := "| Header |\n| ------ |\n| `code` |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableWithLink(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Name | Link |\n| --- | --- |\n| foo | [bar](http://example.com) |"
	want := "| Name | Link                      |\n| ---- | ------------------------- |\n| foo  | [bar](http://example.com) |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableSingleColumn(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Solo |\n| --- |\n| val |"
	want := "| Solo |\n| ---- |\n| val  |\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStrikethroughGFM(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "~~deleted~~"
	want := "~~deleted~~\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
	}
}

func TestTableIdempotent(t *testing.T) {
	md := newTestMarkdownGFM()
	input := "| Left   | Center | Right |\n| :----- | :----: | ----: |\n| a      |   b    |     c |\n| longer |  text  |  here |\n"
	first := render(t, md, input)
	second := render(t, md, first)
	if first != second {
		t.Errorf("table not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// --- Phase 6: Block spacing tests ---

func TestBlockSpacingTightListNoBlankLines(t *testing.T) {
	md := newTestMarkdown()
	input := "- a\n- b\n- c"
	want := "- a\n- b\n- c\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingLooseListBlankLines(t *testing.T) {
	md := newTestMarkdown()
	input := "- a\n\n- b\n\n- c"
	want := "- a\n\n- b\n\n- c\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingTightListFollowedByParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "- tight\n- list\n\nparagraph after"
	want := "- tight\n- list\n\nparagraph after\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingNestedListInLooseItem(t *testing.T) {
	// Prettier removes blank line before nested list inside loose parent.
	md := newTestMarkdown()
	input := "- a\n\n  - b\n\n- c"
	want := "- a\n  - b\n\n- c\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingListItemMultiParagraph(t *testing.T) {
	md := newTestMarkdown()
	input := "- a\n\n  more text\n\n- b"
	want := "- a\n\n  more text\n\n- b\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingConsecutiveHeadings(t *testing.T) {
	md := newTestMarkdown()
	input := "# H1\n\n## H2\n\n### H3"
	want := "# H1\n\n## H2\n\n### H3\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingCodeBlockInLooseList(t *testing.T) {
	md := newTestMarkdown()
	input := "- item\n\n  ```\n  code\n  ```\n\n- next"
	want := "- item\n\n  ```\n  code\n  ```\n\n- next\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrettierIgnore(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			// prettier-ignore suppresses blank line between comment and next element
			name:  "ignore_suppresses_blank_line",
			input: "<!-- prettier-ignore -->\n**bold**",
			want:  "<!-- prettier-ignore -->\n**bold**\n",
		},
		{
			name:  "ignore_followed_by_paragraph",
			input: "<!-- prettier-ignore -->\nsome    ugly     text\n\nNormal paragraph.",
			want:  "<!-- prettier-ignore -->\nsome    ugly     text\n\nNormal paragraph.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestPrettierIgnorePreservesContent(t *testing.T) {
	// The content after prettier-ignore is still formatted by goldmark's AST
	// (we can't skip AST walking for it). But the blank line suppression works.
	md := newTestMarkdown()
	input := "Normal.\n\n<!-- prettier-ignore -->\nAlso normal.\n\nEnd."
	want := "Normal.\n\n<!-- prettier-ignore -->\nAlso normal.\n\nEnd.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrettierIgnoreStartEnd(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "basic_range",
			input: "before\n\n<!-- prettier-ignore-start -->\n\n**ugly**   text\n\n<!-- prettier-ignore-end -->\n\nafter",
			want:  "before\n\n<!-- prettier-ignore-start -->\n\n**ugly**   text\n\n<!-- prettier-ignore-end -->\n\nafter\n",
		},
		{
			name: "range_no_blank_lines_between",
			input: "before\n\n<!-- prettier-ignore-start -->\n**ugly**   text\n<!-- prettier-ignore-end -->\n\nafter",
			want:  "before\n\n<!-- prettier-ignore-start -->\n**ugly**   text\n<!-- prettier-ignore-end -->\n\nafter\n",
		},
		{
			name: "range_preserves_ugly_formatting",
			input: "<!-- prettier-ignore-start -->\n\n#   ugly heading\n\n- ugly   list\n+ another\n\n<!-- prettier-ignore-end -->",
			want:  "<!-- prettier-ignore-start -->\n\n#   ugly heading\n\n- ugly   list\n+ another\n\n<!-- prettier-ignore-end -->\n",
		},
		{
			name: "multiple_ranges",
			input: "<!-- prettier-ignore-start -->\nfirst\n<!-- prettier-ignore-end -->\n\nmiddle\n\n<!-- prettier-ignore-start -->\nsecond\n<!-- prettier-ignore-end -->",
			want:  "<!-- prettier-ignore-start -->\nfirst\n<!-- prettier-ignore-end -->\n\nmiddle\n\n<!-- prettier-ignore-start -->\nsecond\n<!-- prettier-ignore-end -->\n",
		},
		{
			name: "range_at_start_of_doc",
			input: "<!-- prettier-ignore-start -->\nugly\n<!-- prettier-ignore-end -->\n\nnormal",
			want:  "<!-- prettier-ignore-start -->\nugly\n<!-- prettier-ignore-end -->\n\nnormal\n",
		},
		{
			name: "range_at_end_of_doc",
			input: "normal\n\n<!-- prettier-ignore-start -->\nugly\n<!-- prettier-ignore-end -->",
			want:  "normal\n\n<!-- prettier-ignore-start -->\nugly\n<!-- prettier-ignore-end -->\n",
		},
		{
			name: "unmatched_start_ignored",
			input: "before\n\n<!-- prettier-ignore-start -->\n\nugly",
			want:  "before\n\n<!-- prettier-ignore-start -->\n\nugly\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("render() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestPrettierIgnoreStartEndIdempotent(t *testing.T) {
	md := newTestMarkdown()
	input := "before\n\n<!-- prettier-ignore-start -->\n\n**ugly**   text\n\n<!-- prettier-ignore-end -->\n\nafter"
	first := render(t, md, input)
	second := render(t, md, first)
	if first != second {
		t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestBlockSpacingHTMLBlockSequence(t *testing.T) {
	md := newTestMarkdown()
	// Two consecutive HTML blocks without blank line between them in source
	// should not get a blank line.
	input := "<div>\nfoo\n</div>\n<div>\nbar\n</div>"
	want := "<div>\nfoo\n</div>\n<div>\nbar\n</div>\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockSpacingDocumentTrailingNewline(t *testing.T) {
	md := newTestMarkdown()
	// Document should end with exactly one newline.
	input := "Hello."
	want := "Hello.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// --- Phase 7: Delimiter escaping tests ---

func TestEmphasisEscaping(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			// _hello*world_ — goldmark gives us Text("hello*") + Text("world")
			// inside Emphasis. The `*` between letters needs escaping.
			name:  "asterisk_between_letters",
			input: "_hello*world_",
			want:  "_hello\\*world_\n",
		},
		{
			// **a*b** — `*` inside strong between letters needs escaping.
			name:  "asterisk_in_strong",
			input: "**a*b**",
			want:  "**a\\*b**\n",
		},
		{
			// No `*` or `_` in content — no escaping needed.
			name:  "no_delimiters",
			input: "_plain text_",
			want:  "_plain text_\n",
		},
		{
			// _a*b*c_ — inner *b* becomes nested emphasis, not literal.
			name:  "nested_emphasis_not_escaped",
			input: "_a*b*c_",
			want:  "_a*b*c_\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEscapingIdempotent(t *testing.T) {
	md := newTestMarkdown()
	// Escaped output should re-render identically.
	input := "_hello\\*world_"
	first := render(t, md, input)
	second := render(t, md, first)
	if first != second {
		t.Errorf("not idempotent:\nfirst: %q\nsecond: %q", first, second)
	}
}

func TestEscapingInComplexDocument(t *testing.T) {
	md := newTestMarkdown()
	input := "# Title\n\nSome _hello*world_ text.\n\n**a*b** bold."
	want := "# Title\n\nSome _hello\\*world_ text.\n\n**a\\*b** bold.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteParagraphNoBlankPrevLines(t *testing.T) {
	// Goldmark quirk: paragraphs inside blockquotes don't get
	// HasBlankPreviousLines=true. We need special handling.
	md := newTestMarkdown()
	// This input has two paragraphs inside a blockquote WITHOUT blank > lines.
	// Goldmark still parses them as separate paragraphs because of the ">".
	input := "> First paragraph.\n>\n> Second paragraph."
	want := "> First paragraph.\n>\n> Second paragraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeeplyNestedBlockquote(t *testing.T) {
	md := newTestMarkdown()
	input := "> > > deeply nested"
	want := "> > > deeply nested\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// --- Phase 4: List tests ---

func TestGitDiffFriendlyOrderedList(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			// Pattern: 1. 1. 1. → git-diff-friendly
			name:  "repeated_ones",
			input: "1. first\n1. second\n1. third",
			want:  "1. first\n1. second\n1. third\n",
		},
		{
			// Pattern: 0. 1. 1. → git-diff-friendly (starts at 0)
			name:  "zero_start",
			input: "0. zero\n1. one\n1. two",
			want:  "0. zero\n1. one\n1. two\n",
		},
		{
			// Normal sequential: 1. 2. 3. → NOT git-diff-friendly
			name:  "sequential",
			input: "1. first\n2. second\n3. third",
			want:  "1. first\n2. second\n3. third\n",
		},
		{
			// Pattern: 5. 1. 1. → git-diff-friendly (starts at 5)
			name:  "start_at_five",
			input: "5. five\n1. one\n1. two",
			want:  "5. five\n1. one\n1. two\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestAlignedOrderedList(t *testing.T) {
	md := newTestMarkdown(prettier.WithTabWidth(4))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			// "1. " is 3 chars, pad to 4 with tabWidth=4.
			name:  "single_digit_padded",
			input: "1.  first\n2.  second",
			want:  "1.  first\n2.  second\n",
		},
		{
			// "10. " is 4 chars, already aligned to tabWidth=4.
			name:  "double_digit_aligned",
			input: "10. ten\n11. eleven",
			want:  "10. ten\n11. eleven\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestUnorderedListMarkerAlternation(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "top_level_dash",
			input: "- a\n- b",
			want:  "- a\n- b\n",
		},
		{
			// Nesting does NOT alternate — only consecutive sibling lists do.
			// All nested levels use the same marker (determined by sibling index).
			name:  "nested_same_marker",
			input: "- a\n  - b\n    - c",
			want:  "- a\n  - b\n    - c\n",
		},
		{
			// Using + as marker in source — should be normalized to -
			name:  "plus_normalized",
			input: "+ a\n+ b",
			want:  "- a\n- b\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestOrderedListDelimiterAlternation(t *testing.T) {
	md := newTestMarkdown()
	// At top level, even-index lists use ".", odd-index use ")".
	input := "1. first\n2. second"
	want := "1. first\n2. second\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestListWithNestedContent(t *testing.T) {
	md := newTestMarkdown()
	input := "- paragraph one\n\n  paragraph two\n\n- another item"
	want := "- paragraph one\n\n  paragraph two\n\n- another item\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlockquoteWithCodeBlock(t *testing.T) {
	md := newTestMarkdown()
	input := "> Paragraph.\n>\n> ```\n> code\n> ```"
	want := "> Paragraph.\n>\n> ```\n> code\n> ```\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestComplexDocument(t *testing.T) {
	md := newTestMarkdown()
	input := `# Main Title

This is a paragraph with **bold** and _italic_ text.

## Section

- Item one
- Item two
- Item three

> A blockquote with some text.

` + "```" + `go
func main() {
	fmt.Println("hello")
}
` + "```" + `

---

1. First
2. Second
3. Third

End of document.`

	want := `# Main Title

This is a paragraph with **bold** and _italic_ text.

## Section

- Item one
- Item two
- Item three

> A blockquote with some text.

` + "```" + `go
func main() {
	fmt.Println("hello")
}
` + "```" + `

---

1. First
2. Second
3. Third

End of document.
`
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestIdempotent(t *testing.T) {
	// Rendering the output a second time should produce identical results.
	md := newTestMarkdown()
	input := "# Title\n\nParagraph with **bold**.\n\n- item 1\n- item 2\n\n> Quote.\n\n---\n\nEnd.\n"
	first := render(t, md, input)
	second := render(t, md, first)
	if first != second {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// --- proseWrap: "never" tests ---

func TestProseWrapNever(t *testing.T) {
	md := newTestMarkdown(prettier.WithProseWrap(prettier.ProseWrapNever))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "join_soft_line_breaks",
			input: "hello\nworld",
			want:  "hello world\n",
		},
		{
			name:  "multiple_lines",
			input: "one\ntwo\nthree",
			want:  "one two three\n",
		},
		{
			name:  "preserve_hard_line_break",
			input: "hello\\\nworld",
			want:  "hello\\\nworld\n",
		},
		{
			name:  "preserve_blank_line_between_paragraphs",
			input: "first paragraph\nwith wrap\n\nsecond paragraph",
			want:  "first paragraph with wrap\n\nsecond paragraph\n",
		},
		{
			name:  "in_emphasis",
			input: "_hello\nworld_",
			want:  "_hello world_\n",
		},
		{
			name:  "in_blockquote",
			input: "> hello\n> world",
			want:  "> hello world\n",
		},
		{
			name:  "in_list_item",
			input: "- hello\n  world",
			want:  "- hello world\n",
		},
		{
			name:  "preserve_mode_keeps_line_breaks",
			input: "hello\nworld",
			want:  "hello\nworld\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mdToUse goldmark.Markdown
			if tt.name == "preserve_mode_keeps_line_breaks" {
				mdToUse = newTestMarkdown() // default: preserve
			} else {
				mdToUse = md
			}
			got := render(t, mdToUse, tt.input)
			if got != tt.want {
				t.Errorf("render(%q) =\n%q\nwant:\n%q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProseWrapNeverCJK(t *testing.T) {
	md := newTestMarkdown(prettier.WithProseWrap(prettier.ProseWrapNever))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "cj_to_cj_no_space",
			input: "你好\n世界",
			want:  "你好世界\n",
		},
		{
			name:  "latin_to_latin_space",
			input: "hello\nworld",
			want:  "hello world\n",
		},
		{
			name:  "cj_to_latin_space",
			input: "你好\nhello",
			want:  "你好 hello\n",
		},
		{
			name:  "latin_to_cj_space",
			input: "hello\n你好",
			want:  "hello 你好\n",
		},
		{
			name:  "korean_to_korean_space",
			input: "안녕\n하세요",
			want:  "안녕 하세요\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("render(%q) =\n%q\nwant:\n%q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProseWrapNeverCompactTable(t *testing.T) {
	// With a narrow printWidth, tables that exceed it use compact mode.
	md := newTestMarkdownGFM(
		prettier.WithProseWrap(prettier.ProseWrapNever),
		prettier.WithPrintWidth(30),
	)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "wide_table_goes_compact",
			input: "| Header One | Header Two | Header Three |\n| --- | --- | --- |\n| a | b | c |",
			want:  "| Header One | Header Two | Header Three |\n| --- | --- | --- |\n| a | b | c |\n",
		},
		{
			name:  "narrow_table_stays_aligned",
			input: "| A | B |\n| --- | --- |\n| 1 | 2 |",
			want:  "| A   | B   |\n| --- | --- |\n| 1   | 2   |\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("render() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestProseWrapNeverIdempotent(t *testing.T) {
	md := newTestMarkdown(prettier.WithProseWrap(prettier.ProseWrapNever))
	input := "This is a long\nparagraph that\nspans multiple lines.\n\n- list\n  item\n\n> blockquote\n> text"
	first := render(t, md, input)
	second := render(t, md, first)
	if first != second {
		t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}
