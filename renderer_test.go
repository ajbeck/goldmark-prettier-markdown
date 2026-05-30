package prettier_test

import (
	"bytes"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
	"go.abhg.dev/goldmark/wikilink"

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

func newTestMarkdownFootnote(opts ...prettier.Option) goldmark.Markdown {
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
	md.Parser().AddOptions(
		parser.WithBlockParsers(
			util.Prioritized(extension.NewFootnoteBlockParser(), 999),
		),
		parser.WithInlineParsers(
			util.Prioritized(extension.NewFootnoteParser(), 101),
		),
		parser.WithASTTransformers(
			util.Prioritized(extension.NewFootnoteASTTransformer(), 999),
		),
	)
	return md
}

func newTestMarkdownDefList(opts ...prettier.Option) goldmark.Markdown {
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
	md.Parser().AddOptions(
		parser.WithBlockParsers(
			util.Prioritized(extension.NewDefinitionListParser(), 101),
			util.Prioritized(extension.NewDefinitionDescriptionParser(), 102),
		),
	)
	return md
}

func newTestMarkdownWikiLink(opts ...prettier.Option) goldmark.Markdown {
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
	md.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&wikilink.Parser{}, 199),
		),
	)
	return md
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

// Tests below require non-default options (custom printWidth, tabWidth,
// singleQuote) that can't be expressed as golden files with the standard
// variant system. They stay as inline tests.

func TestSingleQuoteOption(t *testing.T) {
	md := newTestMarkdown(prettier.WithSingleQuote(true))
	input := `[text](http://example.com "Title")`
	want := `[text](http://example.com 'Title')` + "\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("Render(%q) = %q, want %q", input, got, want)
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
			name:  "single_digit_padded",
			input: "1.  first\n2.  second",
			want:  "1.  first\n2.  second\n",
		},
		{
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

func TestZeroTabWidthUsesDefault(t *testing.T) {
	md := newTestMarkdown(prettier.WithTabWidth(0))
	input := "1. item"
	want := "1. item\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("render(%q) = %q, want %q", input, got, want)
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
				t.Errorf("render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProseWrapNeverCompactTable(t *testing.T) {
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
				t.Errorf("render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProseWrapAlways(t *testing.T) {
	md := newTestMarkdown(
		prettier.WithProseWrap(prettier.ProseWrapAlways),
		prettier.WithPrintWidth(40),
	)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short_paragraph_no_wrap",
			input: "hello world",
			want:  "hello world\n",
		},
		{
			name:  "wrap_long_paragraph",
			input: "one two three four five six seven eight nine ten eleven twelve",
			want:  "one two three four five six seven eight\nnine ten eleven twelve\n",
		},
		{
			name:  "join_and_rewrap_soft_breaks",
			input: "one\ntwo\nthree four five six seven eight nine ten eleven twelve",
			want:  "one two three four five six seven eight\nnine ten eleven twelve\n",
		},
		{
			name:  "preserve_hard_line_break",
			input: "hello\\\nworld this is a test of wrapping at forty characters",
			want:  "hello\\\nworld this is a test of wrapping at\nforty characters\n",
		},
		{
			name:  "reset_width_after_hard_line_break",
			input: "12345678901234567890\\\na b c d e",
			want:  "12345678901234567890\\\na b c d e\n",
		},
		{
			name:  "preserve_paragraphs",
			input: "first paragraph with many words that go beyond forty columns\n\nsecond paragraph that also has many words beyond forty columns",
			want:  "first paragraph with many words that go\nbeyond forty columns\n\nsecond paragraph that also has many\nwords beyond forty columns\n",
		},
		{
			name:  "wrap_in_blockquote",
			input: "> this is a blockquote with many words that should wrap at the print width",
			want:  "> this is a blockquote with many words\n> that should wrap at the print width\n",
		},
		{
			name:  "wrap_in_list_item",
			input: "- this is a list item with many words that should wrap at the print width",
			want:  "- this is a list item with many words\n  that should wrap at the print width\n",
		},
		{
			name:  "no_wrap_in_atx_heading",
			input: "## this is a heading with many words that exceed the print width limit",
			want:  "## this is a heading with many words that exceed the print width limit\n",
		},
		{
			name:  "syntax_safety_blockquote",
			input: "word word word word word word word word > not a blockquote",
			want:  "word word word word word word word\nword > not a blockquote\n",
		},
		{
			name:  "syntax_safety_heading",
			input: "word word word word word word word word ## not a heading",
			want:  "word word word word word word word\nword ## not a heading\n",
		},
		{
			name:  "syntax_safety_list_marker",
			input: "word word word word word word word word - not a list",
			want:  "word word word word word word word\nword - not a list\n",
		},
		{
			name:  "syntax_safety_ordered_list",
			input: "word word word word word word word word 1. not a list",
			want:  "word word word word word word word\nword 1. not a list\n",
		},
		{
			name:  "link_text_not_broken",
			input: "[this is a very long link text that should not be broken](http://example.com)",
			want:  "[this is a very long link text that should not be broken](http://example.com)\n",
		},
		{
			name:  "emphasis_preserved",
			input: "this is _emphasized text_ in a paragraph with many words that wrap",
			want:  "this is _emphasized text_ in a paragraph\nwith many words that wrap\n",
		},
		{
			name:  "code_span_not_broken",
			input: "this is `inline code that is quite long` in a paragraph with words",
			want:  "this is `inline code that is quite long`\nin a paragraph with words\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("input:  %q\ngot:    %q\nwant:   %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPrettierIgnoreNextPreservesSource(t *testing.T) {
	md := newTestMarkdown()
	input := "<!-- prettier-ignore -->\n#   ugly heading"
	want := "<!-- prettier-ignore -->\n#   ugly heading\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("render(%q) = %q, want %q", input, got, want)
	}
}

func TestRawBlocksPreserveTrailingSpaces(t *testing.T) {
	md := newTestMarkdown()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "fenced_code",
			input: "```\nline   \n```\n",
			want:  "```\nline   \n```\n",
		},
		{
			name:  "indented_code",
			input: "    line   \n",
			want:  "    line   \n",
		},
		{
			name:  "ignored_range",
			input: "<!-- prettier-ignore-start -->\nline   \n<!-- prettier-ignore-end -->\n",
			want:  "<!-- prettier-ignore-start -->\nline   \n<!-- prettier-ignore-end -->\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("render(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProseWrapAlwaysCJK(t *testing.T) {
	md := newTestMarkdown(
		prettier.WithProseWrap(prettier.ProseWrapAlways),
		prettier.WithPrintWidth(20),
	)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "cj_no_break",
			input: "你好世界这是一个测试",
			want:  "你好世界这是一个测试\n",
		},
		{
			name:  "cj_soft_break_removed",
			input: "你好世界\n这是测试",
			want:  "你好世界这是测试\n",
		},
		{
			name:  "korean_breaks_like_latin",
			input: "한국어 테스트 입니다 여기서 줄바꿈",
			want:  "한국어 테스트 입니다\n여기서 줄바꿈\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, md, tt.input)
			if got != tt.want {
				t.Errorf("input:  %q\ngot:    %q\nwant:   %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProseWrapAlwaysSetext(t *testing.T) {
	md := newTestMarkdown(
		prettier.WithProseWrap(prettier.ProseWrapAlways),
		prettier.WithPrintWidth(25),
	)
	got := render(t, md, "this is a long setext heading\n===")
	want := "this is a long setext\nheading\n===\n"
	if got != want {
		t.Errorf("got:  %q\nwant: %q", got, want)
	}
}

func TestProseWrapAlwaysNestedBlockquote(t *testing.T) {
	md := newTestMarkdown(
		prettier.WithProseWrap(prettier.ProseWrapAlways),
		prettier.WithPrintWidth(30),
	)
	got := render(t, md, "> > this is a deeply nested blockquote with long text that wraps")
	want := "> > this is a deeply nested\n> > blockquote with long text\n> > that wraps\n"
	if got != want {
		t.Errorf("got:  %q\nwant: %q", got, want)
	}
}

func TestFootnoteProseWrapNever(t *testing.T) {
	md := newTestMarkdownFootnote(prettier.WithProseWrap(prettier.ProseWrapNever))
	input := "Text[^hello].\n\n[^hello]: this is a long long long long long long long long long long long long long paragraph.\n"
	want := "Text[^hello].\n\n[^hello]: this is a long long long long long long long long long long long long long paragraph.\n"
	got := render(t, md, input)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFootnoteProseWrapAlways(t *testing.T) {
	md := newTestMarkdownFootnote(
		prettier.WithProseWrap(prettier.ProseWrapAlways),
		prettier.WithPrintWidth(80),
	)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short_footnote_stays_inline",
			input: "Text[^hello].\n\n[^hello]: world\n",
			want:  "Text[^hello].\n\n[^hello]: world\n",
		},
		{
			name:  "long_footnote_becomes_block",
			input: "Text[^hello].\n\n[^hello]: this is a long long long long long long long long long long long long long paragraph.\n",
			want:  "Text[^hello].\n\n[^hello]:\n    this is a long long long long long long long long long long long long long\n    paragraph.\n",
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
