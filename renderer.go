// Package prettier is a goldmark renderer that outputs prettier-formatted markdown.
package prettier

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	east "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Renderer renders goldmark AST nodes as prettier-formatted markdown.
type Renderer struct {
	config Config
	helper *renderer.Helper[io.Writer, rendererConfig]
}

type rendererConfig struct {
	renderer.Config[io.Writer, rendererConfig]
}

var _ renderer.Renderer[io.Writer] = (*Renderer)(nil)

var renderRunnerKey = renderer.NewContextKey()

type renderRunner struct {
	config Config
	rc     *renderContext
}

type runnerHandler func(*renderRunner, util.BufWriter, []byte, ast.Node, bool) (ast.WalkStatus, error)

// NewRenderer returns a new Renderer with the given options.
func NewRenderer(opts ...Option) *Renderer {
	cfg := DefaultConfig()
	for _, o := range opts {
		o.SetPrettierOption(&cfg)
	}
	r := &Renderer{config: cfg}
	var builder renderer.HelperBuilder[io.Writer, rendererConfig]
	r.helper = builder.Options(
		renderer.WithNodeRenderers[io.Writer, rendererConfig](r.nodeRenderers()),
	).Build()
	return r
}

// Render renders a parsed Markdown AST.
func (r *Renderer) Render(w io.Writer, source []byte, node ast.Node, opts ...renderer.RenderOption) error {
	return r.helper.Render(w, source, node, opts...)
}

// RenderStringSource renders a parsed Markdown AST from a string source.
func (r *Renderer) RenderStringSource(w io.Writer, source string, node ast.Node, opts ...renderer.RenderOption) error {
	return r.helper.RenderStringSource(w, source, node, opts...)
}

func (r *Renderer) runner(rc renderer.Context) *renderRunner {
	return rc.ComputeIfAbsent(renderRunnerKey, func() any {
		return &renderRunner{config: r.config}
	}).(*renderRunner)
}

func (r *Renderer) nodeRenderer(handler runnerHandler) renderer.NodeRenderer[io.Writer] {
	return renderer.NodeRendererFunc(func(_ io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
		runner := r.runner(rc)
		return handler(runner, runner.rc.w, source, node, entering)
	})
}

func (r *Renderer) documentNodeRenderer() renderer.NodeRenderer[io.Writer] {
	return renderer.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
		runner := r.runner(rc)
		return runner.renderDocument(w, source, node, entering)
	})
}

func (r *Renderer) nodeRenderers() map[ast.NodeKind]renderer.NodeRenderer[io.Writer] {
	return map[ast.NodeKind]renderer.NodeRenderer[io.Writer]{
		ast.KindDocument:               r.documentNodeRenderer(),
		ast.KindHeading:                r.nodeRenderer((*renderRunner).renderHeading),
		ast.KindBlockquote:             r.nodeRenderer((*renderRunner).renderBlockquote),
		ast.KindCodeBlock:              r.nodeRenderer((*renderRunner).renderCodeBlock),
		ast.KindHTMLBlock:              r.nodeRenderer((*renderRunner).renderHTMLBlock),
		ast.KindList:                   r.nodeRenderer((*renderRunner).renderList),
		ast.KindListItem:               r.nodeRenderer((*renderRunner).renderListItem),
		ast.KindParagraph:              r.nodeRenderer((*renderRunner).renderParagraph),
		ast.KindThematicBreak:          r.nodeRenderer((*renderRunner).renderThematicBreak),
		ast.KindAutoLink:               r.nodeRenderer((*renderRunner).renderAutoLink),
		ast.KindCodeSpan:               r.nodeRenderer((*renderRunner).renderCodeSpan),
		ast.KindEmphasis:               r.nodeRenderer((*renderRunner).renderEmphasis),
		ast.KindStrong:                 r.nodeRenderer((*renderRunner).renderStrong),
		ast.KindImage:                  r.nodeRenderer((*renderRunner).renderImage),
		ast.KindLink:                   r.nodeRenderer((*renderRunner).renderLink),
		ast.KindRawHTML:                r.nodeRenderer((*renderRunner).renderRawHTML),
		ast.KindText:                   r.nodeRenderer((*renderRunner).renderText),
		east.KindTable:                 r.nodeRenderer((*renderRunner).renderTable),
		east.KindTableHeader:           r.nodeRenderer((*renderRunner).renderTableHeader),
		east.KindTableRow:              r.nodeRenderer((*renderRunner).renderTableRow),
		east.KindTableCell:             r.nodeRenderer((*renderRunner).renderTableCell),
		east.KindStrikethrough:         r.nodeRenderer((*renderRunner).renderStrikethrough),
		east.KindFootnoteDefinition:    r.nodeRenderer((*renderRunner).renderFootnote),
		east.KindFootnoteReference:     r.nodeRenderer((*renderRunner).renderFootnoteLink),
		east.KindDefinitionList:        r.nodeRenderer((*renderRunner).renderDefinitionList),
		east.KindDefinitionTerm:        r.nodeRenderer((*renderRunner).renderDefinitionTerm),
		east.KindDefinitionDescription: r.nodeRenderer((*renderRunner).renderDefinitionDescription),
	}
}

// renderContext carries mutable state during a single Render call.
type renderContext struct {
	w      *markdownWriter
	source []byte
	config *Config

	// listStack tracks nested list state for marker alternation and numbering.
	listStack []listContext

	// ignoreRanges holds prettier-ignore-start/end pairs found during document scan.
	ignoreRanges []ignoreRange
	// ignoredNodes is the set of nodes inside ignore ranges (plus end comments)
	// that should be skipped entirely during rendering.
	ignoredNodes map[ast.Node]struct{}
	// ignoredNextNodes is the set of nodes following a one-line
	// <!-- prettier-ignore --> directive.
	ignoredNextNodes map[ast.Node]struct{}

	// fillWrapBuf captures inline content for fill-wrapping in "always" mode.
	fillWrapBuf *bytes.Buffer
	// fillWrapWriter is the saved real writer during fill-wrap buffering.
	fillWrapWriter *markdownWriter
	// singleLineDepth tracks nesting in non-breakable contexts (links).
	// When > 0, spaces are not marked as breakable during fill-wrap.
	singleLineDepth int
}

// ignoreRange represents a <!-- prettier-ignore-start --> / <!-- prettier-ignore-end --> pair.
type ignoreRange struct {
	startNode    ast.Node // the start HTML comment
	endNode      ast.Node // the end HTML comment
	betweenStart int      // byte offset in source after start comment
	betweenEnd   int      // byte offset in source at start of end comment
}

type listContext struct {
	list            *ast.List
	num             int  // current ordered list number
	gitDiffFriendly bool // use repeated "1." after first item
	aligned         bool // pad prefix to tabWidth boundaries
}

func newRenderContext(w io.Writer, source []byte, config *Config) *renderContext {
	return &renderContext{
		w:      newMarkdownWriter(w),
		source: source,
		config: config,
	}
}

type sourceSegments []text.Segment

func (s sourceSegments) Len() int              { return len(s) }
func (s sourceSegments) At(i int) text.Segment { return s[i] }

func sourceSegmentsOf(node ast.Node) sourceSegments {
	if block, ok := node.(ast.BlockNode); ok {
		return block.Source()
	}
	return nil
}

func hasBlankPreviousLines(node ast.Node) bool {
	block, ok := node.(ast.BlockNode)
	return ok && block.HasBlankPreviousLines()
}

// --- Block node renderers ---

func (r *renderRunner) renderDocument(w io.Writer, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.rc = newRenderContext(w, source, &r.config)
		r.scanIgnoreRanges(node)
	} else {
		// Trailing newline at end of document.
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

// scanIgnoreRanges scans direct children of the document for
// prettier-ignore-start/end pairs and populates the render context.
func (r *renderRunner) scanIgnoreRanges(doc ast.Node) {
	var startNode ast.Node
	var startEnd int // byte offset at end of start comment

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		kind := prettierIgnoreKind(child, r.rc.source)
		switch kind {
		case "next":
			if next := child.NextSibling(); next != nil {
				if r.rc.ignoredNextNodes == nil {
					r.rc.ignoredNextNodes = make(map[ast.Node]struct{})
				}
				r.rc.ignoredNextNodes[next] = struct{}{}
			}
		case "start":
			if startNode == nil {
				startNode = child
				startEnd = htmlBlockEndOffset(child)
			}
		case "end":
			if startNode != nil {
				endStart := htmlBlockStartOffset(child)
				ir := ignoreRange{
					startNode:    startNode,
					endNode:      child,
					betweenStart: startEnd,
					betweenEnd:   endStart,
				}
				r.rc.ignoreRanges = append(r.rc.ignoreRanges, ir)

				// Mark all nodes between start and end (plus the end node)
				// so they are skipped during rendering.
				if r.rc.ignoredNodes == nil {
					r.rc.ignoredNodes = make(map[ast.Node]struct{})
				}
				for n := startNode.NextSibling(); n != nil; n = n.NextSibling() {
					r.rc.ignoredNodes[n] = struct{}{}
					if n == child {
						break
					}
				}
				startNode = nil
			}
		}
	}
}

// htmlBlockStartOffset returns the byte offset of the first character of an HTML block.
func htmlBlockStartOffset(node ast.Node) int {
	lines := sourceSegmentsOf(node)
	if hb, ok := node.(*ast.HTMLBlock); ok {
		lines = hb.Value.Segments()
	}
	if lines.Len() == 0 {
		return 0
	}
	return lines.At(0).Start
}

// htmlBlockEndOffset returns the byte offset after the last character of an HTML block,
// including the closure line if present.
func htmlBlockEndOffset(node ast.Node) int {
	lines := sourceSegmentsOf(node)
	if hb, ok := node.(*ast.HTMLBlock); ok {
		lines = hb.Value.Segments()
	}
	if lines.Len() == 0 {
		return 0
	}
	return lines.At(lines.Len() - 1).Stop
}

func (r *renderRunner) renderParagraph(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if entering {
		r.writeBlockSeparator(node)
		if r.rc.config.ProseWrap == ProseWrapAlways {
			r.beginFillWrap()
		}
	} else {
		if r.rc.config.ProseWrap == ProseWrapAlways {
			r.endFillWrap()
		}
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.Heading)
	if entering {
		r.writeBlockSeparator(node)
		if n.HeadingKind == ast.HeadingKindATX {
			r.rc.w.WriteBytes(bytes.Repeat([]byte("#"), n.Level))
			if n.HasChildren() {
				r.rc.w.WriteBytes([]byte(" "))
			}
		}
	} else {
		r.rc.w.FlushLine()
		if n.HeadingKind == ast.HeadingKindSetext {
			marker := byte('-')
			if n.Level == 1 {
				marker = '='
			}
			r.rc.w.WriteBytes(bytes.Repeat([]byte{marker}, setextUnderlineWidth(n, source)))
			r.rc.w.FlushLine()
		}
	}
	return ast.WalkContinue, nil
}

func setextUnderlineWidth(node *ast.Heading, source []byte) int {
	width := 3
	for _, segment := range sourceSegmentsOf(node) {
		if length := len(bytes.TrimSpace(segment.Bytes(source))); length > width {
			width = length
		}
	}
	return width
}

func (r *renderRunner) renderBlockquote(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if entering {
		r.writeBlockSeparator(node)
		r.rc.w.PushPrefix([]byte("> "))
	} else {
		r.rc.w.PopPrefix()
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.CodeBlock)
	if n.CodeBlockKind == ast.CodeBlockKindFenced {
		if entering {
			r.writeBlockSeparator(node)

			content := n.Value.Bytes(source)
			fenceLen := max(3, maxContinuousCount(string(content), '`')+1)
			fence := strings.Repeat("`", fenceLen)
			r.rc.w.WriteBytes([]byte(fence))
			if !n.Info.IsEmpty() {
				r.rc.w.WriteBytes(n.Info.Bytes(source))
			}
			r.rc.w.EndLine()
			if len(content) == 0 {
				r.rc.w.EndLine()
			} else {
				r.writeRawBytes(content)
				r.rc.w.FlushLine()
			}
			r.rc.w.WriteBytes([]byte(fence))
		} else {
			r.rc.w.FlushLine()
		}
		return ast.WalkContinue, nil
	}

	if entering {
		r.writeBlockSeparator(node)
		// Extra blank line when indented code follows a list (prettier: shouldPrePrintTripleHardline).
		// Without this, CommonMark parsers treat the indented content as list continuation.
		if prev := node.PreviousSibling(); prev != nil && prev.Kind() == ast.KindList {
			r.rc.w.EndLine()
		}
		r.rc.w.PushPrefix([]byte("    "))
		r.writeRawBytes(n.Value.Bytes(source))
		r.rc.w.FlushLine()
	} else {
		r.rc.w.PopPrefix()
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.HTMLBlock)
	if entering {
		r.writeBlockSeparator(node)
		r.writeRawBytes(n.Value.Bytes(source))
		r.rc.w.FlushLine()
	} else {
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.List)
	if entering {
		r.writeBlockSeparator(node)
		lc := listContext{
			list: n,
			num:  n.Start,
		}
		if n.IsOrdered() {
			lc.gitDiffFriendly = isGitDiffFriendlyOrderedList(n, source)
			lc.aligned = isAlignedOrderedList(n, source, r.rc.config.TabWidth)
		}
		r.rc.listStack = append(r.rc.listStack, lc)
	} else {
		r.rc.listStack = r.rc.listStack[:len(r.rc.listStack)-1]
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.writeBlockSeparator(node)
		lc := &r.rc.listStack[len(r.rc.listStack)-1]

		var prefix []byte
		if lc.list.IsOrdered() {
			nthIdx := nthListSiblingIndex(lc.list, node.Parent())
			delim := byte('.')
			if nthIdx%2 != 0 {
				delim = ')'
			}
			// For git-diff-friendly lists, use 1 for all items after first.
			num := lc.num
			if lc.gitDiffFriendly && node.PreviousSibling() != nil {
				num = 1
			}
			prefix = append(prefix, fmt.Append(nil, num)...)
			prefix = append(prefix, delim, ' ')
			lc.num++

			// Align prefix to tabWidth boundaries when applicable.
			if lc.aligned {
				prefix = alignListPrefix(prefix, r.rc.config.TabWidth)
			}
		} else {
			nthIdx := nthUnorderedListItemMarkerRunIndex(lc.list, node, source)
			marker := byte('-')
			if nthIdx%2 != 0 {
				marker = '*'
			}
			prefix = append(prefix, marker, ' ')
		}

		// First line gets the marker prefix.
		r.rc.w.PushPrefix(prefix, 0, 0)
		// Continuation lines get space padding matching the prefix length.
		r.rc.w.PushPrefix(bytes.Repeat([]byte{' '}, len(prefix)), 1)
		if status, isTask := extension.TaskStatusOf(node); isTask {
			if status == extension.TaskStatusCompleted {
				r.rc.w.WriteBytes([]byte("[x] "))
			} else {
				r.rc.w.WriteBytes([]byte("[ ] "))
			}
		}
	} else {
		// Empty list items have no children to generate content, so the
		// marker prefix is never flushed. Force a newline to emit it.
		if node.FirstChild() == nil {
			r.rc.w.EndLine()
		}
		r.rc.w.PopPrefix()
		r.rc.w.PopPrefix()
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderThematicBreak(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if entering {
		r.writeBlockSeparator(node)
		// Default to "---". In list context, alternate "***" / "---".
		marker := "---"
		for i := len(r.rc.listStack) - 1; i >= 0; i-- {
			lc := r.rc.listStack[i]
			nthIdx := nthListSiblingIndex(lc.list, node.Parent())
			if nthIdx%2 == 0 {
				marker = "***"
			} else {
				marker = "---"
			}
			break
		}
		r.rc.w.WriteBytes([]byte(marker))
	} else {
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

// --- Inline node renderers ---

func (r *renderRunner) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	text := []byte(n.Value.Value(source))

	// Escape `*` and `_` inside emphasis/strong to prevent ambiguous markdown.
	if isInsideEmphasisOrStrong(node) {
		text = escapeEmphasisDelimiters(text, node, source)
	}

	// In "always" mode within a fill-wrap context, mark breakable spaces
	// within the text content so fillWrap can split on them.
	if r.inFillWrap() && r.rc.singleLineDepth == 0 {
		text = markBreakableSpaces(text)
	}

	r.rc.w.WriteBytes(text)
	if n.HardLineBreak() {
		r.rc.w.WriteBytes([]byte("\\"))
		r.rc.w.EndLine()
	} else if n.SoftLineBreak() {
		switch r.rc.config.ProseWrap {
		case ProseWrapNever:
			r.writeSoftLineBreakNever(text, n)
		case ProseWrapAlways:
			r.writeSoftLineBreakAlways(text, n)
		default:
			r.rc.w.EndLine()
		}
	}
	return ast.WalkContinue, nil
}

// writeSoftLineBreakNever converts a soft line break to a space (or empty
// for CJ-to-CJ transitions) when proseWrap is "never".
func (r *renderRunner) writeSoftLineBreakNever(text []byte, n *ast.Text) {
	// Get the last rune of the current text.
	preceding, _ := utf8.DecodeLastRune(text)

	// Get the first rune of the next sibling's text.
	var following rune
	if next := n.NextSibling(); next != nil {
		if nextText, ok := next.(*ast.Text); ok {
			following, _ = utf8.DecodeRune([]byte(nextText.Value.Value(r.rc.source)))
		}
	}

	if lineBreakCanBeConvertedToSpace(preceding, following) {
		r.rc.w.WriteBytes([]byte(" "))
	}
	// else: CJ-to-CJ — emit nothing
}

// writeSoftLineBreakAlways converts a soft line break to a breakable space
// (sentinel) or non-breakable space/empty based on CJK context and syntax
// safety.
func (r *renderRunner) writeSoftLineBreakAlways(text []byte, n *ast.Text) {
	preceding, _ := utf8.DecodeLastRune(text)

	var following rune
	var nextFirstWord string
	if next := n.NextSibling(); next != nil {
		if nextText, ok := next.(*ast.Text); ok {
			nextVal := []byte(nextText.Value.Value(r.rc.source))
			following, _ = utf8.DecodeRune(nextVal)
			// Extract the first word to check syntax safety.
			nextRunes := []rune(string(nextVal))
			nextFirstWord = extractNextWord(nextRunes, 0)
		}
	}

	if !lineBreakCanBeConvertedToSpace(preceding, following) {
		return // CJ-to-CJ — emit nothing
	}

	// If the next word would create block syntax, use a non-breakable space.
	if isSyntaxUnsafeWord(nextFirstWord) {
		r.rc.w.WriteByte(' ')
		return
	}

	r.writeBreakableSpace()
}

func (r *renderRunner) renderEmphasis(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Emphasis)
	marker := r.emphasisMarker(n, source)
	r.rc.w.WriteBytes([]byte{marker})
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderStrong(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	r.rc.w.WriteBytes([]byte("**"))
	return ast.WalkContinue, nil
}

// emphasisMarker returns '_' or '*' for a level-1 emphasis node, applying
// prettier's marker selection rules.
func (r *renderRunner) emphasisMarker(n *ast.Emphasis, source []byte) byte {
	// Rule 1: if the first child is an autolink, preserve the original marker.
	if first := n.FirstChild(); first != nil && first.Kind() == ast.KindAutoLink {
		return r.originalEmphasisMarker(n, source)
	}

	// Rule 2: adjacent word without punctuation boundary.
	// Use '*' because '_' between non-punctuation chars is not parsed as emphasis.
	if hasAdjacentWordWithoutPunctuation(n, source) {
		return '*'
	}

	// Rule 3: inside a strong that itself has adjacent words.
	if parent := n.Parent(); parent != nil {
		if pe, ok := parent.(*ast.Strong); ok {
			if hasAdjacentWordWithoutPunctuation(pe, source) {
				return '*'
			}
		}
	}

	// Rule 4: nested inside another emphasis.
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.Emphasis); ok {
			return '*'
		}
	}

	return '_'
}

// originalEmphasisMarker reads the marker character from the source for the
// emphasis node. Falls back to '_'.
func (r *renderRunner) originalEmphasisMarker(n *ast.Emphasis, source []byte) byte {
	// Walk inline children to find the position in source.
	if first := n.FirstChild(); first != nil {
		if t, ok := first.(*ast.Text); ok {
			seg := t.Value.Index()
			// The emphasis marker is just before the first child's text.
			if seg.Start > 0 {
				ch := source[seg.Start-1]
				if ch == '*' || ch == '_' {
					return ch
				}
			}
		}
	}
	return '_'
}

// hasAdjacentWordWithoutPunctuation checks if the inline node has a previous
// or next sibling whose adjacent text character is not whitespace or punctuation.
// This detects cases like "1_2_3" where underscores between non-punctuation
// chars would not be parsed as emphasis.
func hasAdjacentWordWithoutPunctuation(node ast.Node, source []byte) bool {
	if prev := node.PreviousSibling(); prev != nil {
		if ch, ok := lastCharOf(prev, source); ok && !isWhitespaceOrPunctuation(ch) {
			return true
		}
	}
	if next := node.NextSibling(); next != nil {
		if ch, ok := firstCharOf(next, source); ok && !isWhitespaceOrPunctuation(ch) {
			return true
		}
	}
	return false
}

// lastCharOf returns the last character of an inline node's text content.
func lastCharOf(node ast.Node, source []byte) (rune, bool) {
	switch n := node.(type) {
	case *ast.Text:
		v := []byte(n.Value.Value(source))
		if len(v) > 0 {
			return rune(v[len(v)-1]), true
		}
	case *ast.CodeSpan:
		v := n.Value.Bytes(source)
		if len(v) > 0 {
			return rune(v[len(v)-1]), true
		}
	case *ast.Emphasis, *ast.Strong, *ast.Link, *ast.Image:
		// Walk to last leaf.
		for c := node.LastChild(); c != nil; c = c.LastChild() {
			if r, ok := lastCharOf(c, source); ok {
				return r, ok
			}
		}
	}
	return 0, false
}

// firstCharOf returns the first character of an inline node's text content.
func firstCharOf(node ast.Node, source []byte) (rune, bool) {
	switch n := node.(type) {
	case *ast.Text:
		v := []byte(n.Value.Value(source))
		if len(v) > 0 {
			return rune(v[0]), true
		}
	case *ast.CodeSpan:
		v := n.Value.Bytes(source)
		if len(v) > 0 {
			return rune(v[0]), true
		}
	case *ast.Emphasis, *ast.Strong, *ast.Link, *ast.Image:
		// Walk to first leaf.
		for c := node.FirstChild(); c != nil; c = c.FirstChild() {
			if r, ok := firstCharOf(c, source); ok {
				return r, ok
			}
		}
	}
	return 0, false
}

// isWhitespaceOrPunctuation returns whether r is whitespace or a CommonMark
// punctuation character.
func isWhitespaceOrPunctuation(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	return isPunctuation(r)
}

// isPunctuation returns whether r is a CommonMark punctuation character.
// This includes ASCII punctuation and Unicode punctuation categories
// (Pc, Pd, Pe, Pf, Pi, Po, Ps).
func isPunctuation(r rune) bool {
	// ASCII punctuation: !"#$%&'()*+,-./:;<=>?@[\]^_`{|}~
	if r >= '!' && r <= '~' && !isASCIILetterOrDigit(r) {
		return true
	}
	// Unicode punctuation categories.
	return unicode.In(r,
		unicode.Pc, // Connector
		unicode.Pd, // Dash
		unicode.Pe, // Close
		unicode.Pf, // Final
		unicode.Pi, // Initial
		unicode.Po, // Other
		unicode.Ps, // Open
	)
}

func isASCIILetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// --- CJK character classification ---
// These classify characters into the four kinds used by prettier for whitespace
// handling. Used for emphasis marker selection and proseWrap line break conversion.

// WordKind classifies a character for CJK-aware text processing.
type WordKind int

const (
	KindNonCJK         WordKind = iota // Latin, numbers, ASCII punctuation
	KindCJLetter                       // Chinese/Japanese (Han, Katakana, Hiragana, Bopomofo)
	KindKLetter                        // Korean (Hangul)
	KindCJKPunctuation                 // CJK-specific punctuation
)

// ClassifyRune returns the WordKind for a rune.
func ClassifyRune(r rune) WordKind {
	if isPunctuation(r) && isCJKRange(r) {
		return KindCJKPunctuation
	}
	if unicode.Is(unicode.Hangul, r) {
		return KindKLetter
	}
	if isCJKRange(r) {
		return KindCJLetter
	}
	return KindNonCJK
}

// isCJKRange returns true if the rune falls in CJK script ranges.
func isCJKRange(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Katakana,
		unicode.Hiragana,
		unicode.Hangul,
		unicode.Bopomofo,
	)
}

// lineBreakCanBeConvertedToSpace returns true if a soft line break between the
// preceding and following runes can be replaced with a space. This implements
// prettier's lineBreakCanBeConvertedToSpace from whitespace.js.
//
// The general rule: CJ-to-CJ line breaks are removed (not replaced with space)
// because CJ languages don't use spaces between words. All other combinations
// get a space.
func lineBreakCanBeConvertedToSpace(preceding, following rune) bool {
	if preceding == 0 || following == 0 {
		return true
	}

	prevKind := ClassifyRune(preceding)
	nextKind := ClassifyRune(following)

	isNonCJKOrK := func(k WordKind) bool {
		return k == KindNonCJK || k == KindKLetter
	}

	// Non-CJK/Korean to Non-CJK/Korean: always space.
	if isNonCJKOrK(prevKind) && isNonCJKOrK(nextKind) {
		return true
	}

	// Korean ↔ CJ: always space.
	if (prevKind == KindKLetter && nextKind == KindCJLetter) ||
		(prevKind == KindCJLetter && nextKind == KindKLetter) {
		return true
	}

	// Around CJK punctuation or between CJ letters: no space.
	if prevKind == KindCJKPunctuation || nextKind == KindCJKPunctuation ||
		(prevKind == KindCJLetter && nextKind == KindCJLetter) {
		return false
	}

	// Between CJ and ASCII punctuation: space.
	if isASCIIPunctuation(following) || isASCIIPunctuation(preceding) {
		return true
	}

	// Default for CJ ↔ non-CJK: space (simplified from prettier's
	// isInSentenceWithCJSpaces heuristic which examines the full sentence).
	return true
}

// isASCIIPunctuation returns true for ASCII punctuation characters that prettier
// treats as allowing a space when adjacent to CJ characters.
func isASCIIPunctuation(r rune) bool {
	return r >= '!' && r <= '~' && !isASCIILetterOrDigit(r)
}

// isInsideEmphasisOrStrong returns true if the node has an emphasis ancestor.
func isInsideEmphasisOrStrong(node ast.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		switch p.(type) {
		case *ast.Emphasis, *ast.Strong:
			return true
		}
	}
	return false
}

// escapeEmphasisDelimiters escapes `*` and `_` characters in text that could
// form flanking delimiter runs when rendered inside emphasis/strong.
//
// Matches prettier's word.js escaping logic: for each delimiter character,
// determine the effective preceding and following non-whitespace characters
// (looking across whitespace boundaries, as prettier splits text into words).
// If the delimiter could open or close emphasis per CommonMark rules, escape it.
func escapeEmphasisDelimiters(text []byte, node ast.Node, source []byte) []byte {
	if !bytes.ContainsAny(text, "*_") {
		return text
	}

	var result []byte
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch != '*' && ch != '_' {
			result = append(result, ch)
			continue
		}

		// Count preceding backslashes to detect already-escaped delimiters.
		backslashes := 0
		for j := len(result) - 1; j >= 0 && result[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			// Odd number of preceding backslashes means this delimiter is
			// already escaped — don't double-escape.
			result = append(result, ch)
			continue
		}

		// Collect the full delimiter run.
		runStart := i
		for i+1 < len(text) && text[i+1] == ch {
			i++
		}
		run := text[runStart : i+1]

		// Find the effective preceding character (skip whitespace to find
		// the last non-space, matching prettier's word-boundary behavior).
		preceding := effectivePrecedingChar(text, runStart, node, source)
		// Find the effective following character.
		following := effectiveFollowingChar(text, i, node, source)

		if canOpenOrClose(preceding, rune(ch), following) {
			result = append(result, '\\')
		}
		result = append(result, run...)
	}
	return result
}

// effectivePrecedingChar finds the non-whitespace character before position pos
// in text, or looks at the previous sibling node's last character.
func effectivePrecedingChar(text []byte, pos int, node ast.Node, source []byte) rune {
	// Look backward in text, skipping whitespace.
	for j := pos - 1; j >= 0; j-- {
		r := rune(text[j])
		if !unicode.IsSpace(r) {
			return r
		}
	}
	// Look at previous sibling's last character.
	if prev := node.PreviousSibling(); prev != nil {
		if ch, ok := lastCharOf(prev, source); ok {
			return ch
		}
	}
	return 0 // no preceding character (start of emphasis)
}

// effectiveFollowingChar finds the non-whitespace character after position pos
// in text, or looks at the next sibling node's first character.
func effectiveFollowingChar(text []byte, pos int, node ast.Node, source []byte) rune {
	// Look forward in text, skipping whitespace.
	for j := pos + 1; j < len(text); j++ {
		r := rune(text[j])
		if !unicode.IsSpace(r) {
			return r
		}
	}
	// Look at next sibling's first character.
	if next := node.NextSibling(); next != nil {
		if ch, ok := firstCharOf(next, source); ok {
			return ch
		}
	}
	return 0 // no following character (end of emphasis)
}

// canOpenOrClose determines if a delimiter character at the given position
// could form a flanking delimiter run per CommonMark 0.31.2.
func canOpenOrClose(preceding, delimiter, following rune) bool {
	// If we can't determine context, don't escape (same as prettier returning null).
	if preceding == 0 || following == 0 {
		return false
	}

	followedByWS := isUnicodeWhitespace(following)
	precededByWS := isUnicodeWhitespace(preceding)
	followedByPunct := isPunctuation(following)
	precededByPunct := isPunctuation(preceding)

	leftFlanking := !followedByWS &&
		(!followedByPunct || (precededByWS || precededByPunct))
	rightFlanking := !precededByWS &&
		(!precededByPunct || (followedByWS || followedByPunct))

	if delimiter == '*' {
		return leftFlanking || rightFlanking
	}

	// For '_': stricter rules.
	if leftFlanking {
		return !rightFlanking || precededByPunct
	}
	if rightFlanking {
		return !leftFlanking || followedByPunct
	}
	return false
}

// isUnicodeWhitespace returns true for Unicode space separators and ASCII whitespace.
func isUnicodeWhitespace(r rune) bool {
	return unicode.Is(unicode.Zs, r) || r == '\t' || r == '\n' || r == '\f' || r == '\r'
}

func (r *renderRunner) renderCodeSpan(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		content := node.(*ast.CodeSpan).Value.Bytes(source)

		backtickLen := minNotPresentContinuousCount(string(content), '`')
		ticks := strings.Repeat("`", backtickLen)

		beginsWithSpace := len(content) > 0 && unicode.IsSpace(rune(content[0]))
		endsWithSpace := len(content) > 0 && unicode.IsSpace(rune(content[len(content)-1]))
		beginsWithTick := len(content) > 0 && content[0] == '`'
		endsWithTick := len(content) > 0 && content[len(content)-1] == '`'
		onlySpace := len(content) > 0 && bytes.TrimFunc(content, unicode.IsSpace) == nil

		pad := ""
		if beginsWithTick || endsWithTick || (beginsWithSpace && endsWithSpace && !onlySpace) {
			pad = " "
		}

		r.rc.w.WriteBytes([]byte(ticks + pad))
		r.rc.w.WriteBytes(content)
		r.rc.w.WriteBytes([]byte(pad + ticks))

		return ast.WalkSkipChildren, nil
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if entering {
		if r.inFillWrap() {
			r.rc.singleLineDepth++
		}
		r.rc.w.WriteBytes([]byte("["))
	} else {
		r.rc.w.WriteBytes([]byte("]("))
		r.writeURL(n.Destination.Bytes(source), ")")
		r.writeLinkTitle(n.Title.Bytes(source))
		r.rc.w.WriteBytes([]byte(")"))
		if r.inFillWrap() {
			r.rc.singleLineDepth--
		}
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Image)
	if entering {
		r.rc.w.WriteBytes([]byte("!["))
	} else {
		r.rc.w.WriteBytes([]byte("]("))
		r.writeURL(n.Destination.Bytes(source), ")")
		r.writeLinkTitle(n.Title.Bytes(source))
		r.rc.w.WriteBytes([]byte(")"))
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.AutoLink)
	if entering {
		r.rc.w.WriteBytes([]byte("<"))
		r.rc.w.WriteBytes(n.Destination.Bytes(source))
	} else {
		r.rc.w.WriteBytes([]byte(">"))
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*ast.RawHTML)
		r.rc.w.WriteBytes(n.Value.Bytes(source))
	}
	return ast.WalkContinue, nil
}

// --- GFM extension node renderers ---

func (r *renderRunner) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	r.writeBlockSeparator(node)
	// Pass 1: collect cell text and measure column widths.
	var rows [][]cellInfo
	var colWidths []int

	var rowNodes []ast.Node
	for section := node.FirstChild(); section != nil; section = section.NextSibling() {
		if section.Kind() == east.KindTableBody {
			for row := section.FirstChild(); row != nil; row = row.NextSibling() {
				rowNodes = append(rowNodes, row)
			}
			continue
		}
		rowNodes = append(rowNodes, section)
	}
	var alignments []east.Alignment
	for _, row := range rowNodes {
		var rowCells []cellInfo
		colIdx := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			if len(rows) == 0 {
				if tableCell, ok := cell.(*east.TableCell); ok {
					alignments = append(alignments, tableCell.Alignment)
				}
			}
			text := r.renderCellContent(cell, source)
			width := len(text) // TODO: proper display width for CJK
			if colIdx >= len(colWidths) {
				colWidths = append(colWidths, max(3, width))
			} else if width > colWidths[colIdx] {
				colWidths[colIdx] = width
			}
			rowCells = append(rowCells, cellInfo{text: text, width: width})
			colIdx++
		}
		rows = append(rows, rowCells)
	}

	if len(rows) == 0 {
		return ast.WalkSkipChildren, nil
	}

	// In "never" mode, use compact table (no padding) when the aligned
	// table exceeds printWidth (prettier: group(ifBreak(compact, aligned))).
	// PrintWidth <= 0 means unlimited — never go compact.
	compact := false
	if r.rc.config.ProseWrap == ProseWrapNever && r.rc.config.PrintWidth > 0 {
		alignedWidth := tableRowWidth(colWidths)
		if alignedWidth > r.rc.config.PrintWidth {
			compact = true
		}
	}

	// Pass 2: format and output.
	// Header row.
	r.writeTableRow(rows[0], colWidths, alignments, compact)

	// Alignment row.
	r.writeAlignmentRow(colWidths, alignments, compact)

	// Data rows.
	for _, row := range rows[1:] {
		r.writeTableRow(row, colWidths, alignments, compact)
	}

	return ast.WalkSkipChildren, nil
}

// renderCellContent renders the inline content of a table cell to a string.
func (r *renderRunner) renderCellContent(cell ast.Node, source []byte) string {
	var buf bytes.Buffer
	origWriter := r.rc.w
	r.rc.w = newMarkdownWriter(&buf)

	for c := cell.FirstChild(); c != nil; c = c.NextSibling() {
		ast.Walk(c, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			switch n := n.(type) {
			case *ast.Text:
				if entering {
					r.rc.w.WriteBytes([]byte(n.Value.Value(source)))
				}
			case *ast.Emphasis:
				marker := r.emphasisMarker(n, source)
				r.rc.w.WriteBytes([]byte{marker})
			case *ast.Strong:
				r.rc.w.WriteBytes([]byte("**"))
			case *ast.CodeSpan:
				if entering {
					content := n.Value.Bytes(source)
					// Escape pipes in table cells.
					content = bytes.ReplaceAll(content, []byte("|"), []byte(`\|`))
					backtickLen := minNotPresentContinuousCount(string(content), '`')
					ticks := strings.Repeat("`", backtickLen)
					beginsWithTick := len(content) > 0 && content[0] == '`'
					endsWithTick := len(content) > 0 && content[len(content)-1] == '`'
					beginsWithSpace := len(content) > 0 && unicode.IsSpace(rune(content[0]))
					endsWithSpace := len(content) > 0 && unicode.IsSpace(rune(content[len(content)-1]))
					onlySpace := len(content) > 0 && bytes.TrimFunc(content, unicode.IsSpace) == nil
					pad := ""
					if beginsWithTick || endsWithTick || (beginsWithSpace && endsWithSpace && !onlySpace) {
						pad = " "
					}
					r.rc.w.WriteBytes([]byte(ticks + pad))
					r.rc.w.WriteBytes(content)
					r.rc.w.WriteBytes([]byte(pad + ticks))
					return ast.WalkSkipChildren, nil
				}
			case *ast.Link:
				if entering {
					r.rc.w.WriteBytes([]byte("["))
				} else {
					r.rc.w.WriteBytes([]byte("]("))
					r.writeURL(n.Destination.Bytes(source), ")")
					r.writeLinkTitle(n.Title.Bytes(source))
					r.rc.w.WriteBytes([]byte(")"))
				}
			case *ast.Image:
				if entering {
					r.rc.w.WriteBytes([]byte("!["))
				} else {
					r.rc.w.WriteBytes([]byte("]("))
					r.writeURL(n.Destination.Bytes(source), ")")
					r.writeLinkTitle(n.Title.Bytes(source))
					r.rc.w.WriteBytes([]byte(")"))
				}
			case *ast.AutoLink:
				if entering {
					r.rc.w.WriteBytes([]byte("<"))
					r.rc.w.WriteBytes(n.Destination.Bytes(source))
				} else {
					r.rc.w.WriteBytes([]byte(">"))
				}
			case *ast.RawHTML:
				if entering {
					r.rc.w.WriteBytes(n.Value.Bytes(source))
				}
			case *east.Strikethrough:
				r.rc.w.WriteBytes([]byte("~~"))
			}
			return ast.WalkContinue, nil
		})
	}
	r.rc.w.FlushLine()
	r.rc.w = origWriter

	// Trim trailing newline from the rendered content.
	result := buf.String()
	result = strings.TrimRight(result, "\n")
	return result
}

type cellInfo struct {
	text  string
	width int
}

// tableRowWidth returns the total character width of an aligned table row
// given the column widths: `| ` + content + ` |` for each column.
func tableRowWidth(colWidths []int) int {
	// "| col1 | col2 | col3 |" → 1 + sum(1 + width + 1 + 1) for each col
	width := 1 // leading "|"
	for _, cw := range colWidths {
		width += 1 + cw + 1 + 1 // " " + content + " " + "|"
	}
	return width
}

func (r *renderRunner) writeTableRow(cells []cellInfo, colWidths []int, alignments []east.Alignment, compact bool) {
	r.rc.w.WriteBytes([]byte("|"))
	for i, cell := range cells {
		if compact {
			r.rc.w.WriteBytes([]byte(" "))
			r.rc.w.WriteBytes([]byte(cell.text))
			r.rc.w.WriteBytes([]byte(" |"))
		} else {
			width := colWidths[i]
			align := east.AlignNone
			if i < len(alignments) {
				align = alignments[i]
			}
			spaces := width - cell.width
			before := 0
			if align == east.AlignRight {
				before = spaces
			} else if align == east.AlignCenter {
				before = spaces / 2
			}
			after := spaces - before
			r.rc.w.WriteBytes([]byte(" "))
			r.rc.w.WriteBytes(bytes.Repeat([]byte(" "), before))
			r.rc.w.WriteBytes([]byte(cell.text))
			r.rc.w.WriteBytes(bytes.Repeat([]byte(" "), after))
			r.rc.w.WriteBytes([]byte(" |"))
		}
	}
	r.rc.w.EndLine()
}

func (r *renderRunner) writeAlignmentRow(colWidths []int, alignments []east.Alignment, compact bool) {
	r.rc.w.WriteBytes([]byte("|"))
	for i, width := range colWidths {
		align := east.AlignNone
		if i < len(alignments) {
			align = alignments[i]
		}
		first := byte('-')
		last := byte('-')
		if align == east.AlignCenter || align == east.AlignLeft {
			first = ':'
		}
		if align == east.AlignCenter || align == east.AlignRight {
			last = ':'
		}
		if compact {
			// Minimum width: 3 characters (e.g., "---", ":--", ":-:", "--:")
			r.rc.w.WriteBytes([]byte{' ', first, '-', last, ' ', '|'})
		} else {
			r.rc.w.WriteBytes([]byte{' ', first})
			r.rc.w.WriteBytes(bytes.Repeat([]byte("-"), width-2))
			r.rc.w.WriteBytes([]byte{last, ' ', '|'})
		}
	}
	r.rc.w.EndLine()
}

// renderTableHeader, renderTableRow, renderTableCell are no-ops because
// renderTable handles all children via WalkSkipChildren + manual walk.

func (r *renderRunner) renderTableHeader(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderTableRow(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderTableCell(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderStrikethrough(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	r.rc.w.WriteBytes([]byte("~~"))
	return ast.WalkContinue, nil
}

// --- Footnote extension node renderers ---

func (r *renderRunner) renderFootnote(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	fn := node.(*east.FootnoteDefinition)
	if entering {
		r.writeBlockSeparator(node)
		ref := fn.Label.Bytes(source)
		r.rc.w.WriteBytes([]byte("[^"))
		r.rc.w.WriteBytes(ref)
		r.rc.w.WriteBytes([]byte("]"))

		first := fn.FirstChild()
		if r.canInlineFirstChild(fn) && first != nil && first.Kind() == ast.KindBlockquote {
			r.rc.w.WriteBytes([]byte(": > "))
			if err := r.renderInlineChildren(first.FirstChild(), source); err != nil {
				return ast.WalkStop, err
			}
			r.rc.w.FlushLine()
			return ast.WalkSkipChildren, nil
		}

		if r.shouldInlineFootnote(fn) {
			// Inline form: [^ref]: content (always fits, no block fallback).
			r.rc.w.WriteBytes([]byte(": "))
		} else if r.canInlineFirstChild(fn) {
			// First child fits inline: [^ref]: first_child
			// Continuation lines get 4-space indent.
			r.rc.w.WriteBytes([]byte(": "))
			r.rc.w.PushPrefix([]byte("    "), 1)
		} else {
			// Block form: [^ref]:\n    content
			r.rc.w.WriteBytes([]byte(":"))
			r.rc.w.EndLine()
			r.rc.w.PushPrefix([]byte("    "))
		}
	} else {
		first := fn.FirstChild()
		if r.canInlineFirstChild(fn) && first != nil && first.Kind() == ast.KindBlockquote {
			return ast.WalkContinue, nil
		}
		if !r.shouldInlineFootnote(fn) {
			r.rc.w.PopPrefix()
		}
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderInlineChildren(node ast.Node, source []byte) error {
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		if err := ast.Walk(c, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			switch n.Kind() {
			case ast.KindText:
				return r.renderText(r.rc.w, source, n, entering)
			case ast.KindEmphasis:
				return r.renderEmphasis(r.rc.w, source, n, entering)
			case ast.KindStrong:
				return r.renderStrong(r.rc.w, source, n, entering)
			case ast.KindCodeSpan:
				return r.renderCodeSpan(r.rc.w, source, n, entering)
			case ast.KindLink:
				return r.renderLink(r.rc.w, source, n, entering)
			case ast.KindImage:
				return r.renderImage(r.rc.w, source, n, entering)
			case ast.KindAutoLink:
				return r.renderAutoLink(r.rc.w, source, n, entering)
			case ast.KindRawHTML:
				return r.renderRawHTML(r.rc.w, source, n, entering)
			case east.KindStrikethrough:
				return r.renderStrikethrough(r.rc.w, source, n, entering)
			default:
				return ast.WalkContinue, nil
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

// shouldInlineFootnote returns true when the footnote should always be rendered
// inline (no width check, no block fallback). Matches prettier's logic.
func (r *renderRunner) shouldInlineFootnote(fn *east.FootnoteDefinition) bool {
	if fn.ChildCount() != 1 {
		return false
	}
	first := fn.FirstChild()
	if first.Kind() != ast.KindParagraph {
		return false
	}
	switch r.rc.config.ProseWrap {
	case ProseWrapNever:
		return true
	case ProseWrapPreserve:
		return isSingleLineParagraph(first, r.rc.source)
	default:
		return false
	}
}

// isSingleLineParagraph reports whether a paragraph's source text spans a single line.
func isSingleLineParagraph(para ast.Node, source []byte) bool {
	lines := sourceSegmentsOf(para)
	if lines.Len() <= 1 {
		return true
	}
	// Multi-line in source means multi-line paragraph.
	return false
}

// canInlineFirstChild checks if the first child (a paragraph) of a non-inline
// footnote can be rendered on the same line as [^ref]: . This implements
// prettier's group([softline, first_child]) behavior.
func (r *renderRunner) canInlineFirstChild(fn *east.FootnoteDefinition) bool {
	first := fn.FirstChild()
	if first == nil {
		return false
	}
	var contentWidth int
	switch first.Kind() {
	case ast.KindParagraph:
		// In preserve mode, multi-line paragraphs can't be inlined because
		// we'd need to preserve the line breaks.
		if r.rc.config.ProseWrap == ProseWrapPreserve && !isSingleLineParagraph(first, r.rc.source) {
			return false
		}
		contentWidth = r.estimateParagraphFlatWidth(first)
	case ast.KindBlockquote:
		width, ok := r.estimateFootnoteBlockquoteFlatWidth(first)
		if !ok {
			return false
		}
		contentWidth = width
	default:
		return false
	}
	if r.rc.config.PrintWidth <= 0 {
		return true // unlimited width
	}
	prefixLen := len("[^") + len(fn.Label.Bytes(r.rc.source)) + len("]: ")
	return prefixLen+contentWidth <= r.rc.config.PrintWidth
}

func (r *renderRunner) estimateFootnoteBlockquoteFlatWidth(blockquote ast.Node) (int, bool) {
	if blockquote.ChildCount() != 1 || blockquote.FirstChild().Kind() != ast.KindParagraph {
		return 0, false
	}
	paragraph := blockquote.FirstChild()
	if r.rc.config.ProseWrap == ProseWrapPreserve && !isSingleLineParagraph(paragraph, r.rc.source) {
		return 0, false
	}
	return len("> ") + r.estimateParagraphFlatWidth(paragraph), true
}

// estimateParagraphFlatWidth estimates the display width of a paragraph if
// rendered on a single line (all soft breaks → spaces).
func (r *renderRunner) estimateParagraphFlatWidth(para ast.Node) int {
	width := 0
	for c := para.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.Text:
			width += displayWidth(n.Value.Value(r.rc.source))
			if n.SoftLineBreak() && c.NextSibling() != nil {
				width++ // space replacing soft break
			}
		case *ast.CodeSpan:
			// Rough estimate: backticks + content
			width += 2 + len(n.Value.Bytes(r.rc.source))
		case *ast.Emphasis:
			width += 2 // _ on each side
			// Add child content width recursively (simplified).
			for cc := n.FirstChild(); cc != nil; cc = cc.NextSibling() {
				if t, ok := cc.(*ast.Text); ok {
					width += displayWidth(t.Value.Value(r.rc.source))
				}
			}
		case *ast.Strong:
			width += 4
			for cc := n.FirstChild(); cc != nil; cc = cc.NextSibling() {
				if t, ok := cc.(*ast.Text); ok {
					width += displayWidth(t.Value.Value(r.rc.source))
				}
			}
		case *ast.Link:
			width += 4 // []()
			width += len(n.Destination.Bytes(r.rc.source))
			for cc := n.FirstChild(); cc != nil; cc = cc.NextSibling() {
				if t, ok := cc.(*ast.Text); ok {
					width += displayWidth(t.Value.Value(r.rc.source))
				}
			}
		default:
			// Conservative fallback for unknown inline types.
			width += 10
		}
	}
	return width
}

func (r *renderRunner) renderFootnoteLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fn := node.(*east.FootnoteReference)
	ref := fn.Label.Bytes(source)
	r.rc.w.WriteBytes([]byte("[^"))
	r.rc.w.WriteBytes(ref)
	r.rc.w.WriteBytes([]byte("]"))
	return ast.WalkContinue, nil
}

// --- Definition list extension node renderers ---

func (r *renderRunner) renderDefinitionList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if entering {
		r.writeBlockSeparator(node)
	} else {
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderDefinitionTerm(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		// Blank line before a term when preceded by a description (visual grouping).
		if prev := node.PreviousSibling(); prev != nil && prev.Kind() == east.KindDefinitionDescription {
			r.rc.w.EndLine()
		}
	} else {
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *renderRunner) renderDefinitionDescription(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		// Loose definitions have a blank line between term and description.
		dd := node.(*east.DefinitionDescription)
		if !dd.IsTight && hasBlankPreviousLines(node) {
			r.rc.w.EndLine()
		}
		// ": " on the first line, "    " continuation on subsequent lines.
		r.rc.w.PushPrefix([]byte(": "), 0, 0)
		r.rc.w.PushPrefix([]byte("    "), 1)
	} else {
		r.rc.w.PopPrefix()
		r.rc.w.PopPrefix()
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

// --- Fill-wrap for proseWrap "always" ---

// fillWrapSentinel is a zero byte used to mark breakable space positions
// in the fill-wrap buffer. fillWrap splits on this to identify wrap candidates.
const fillWrapSentinel = '\x00'

// beginFillWrap starts capturing inline output into a buffer for later
// fill-wrapping. Call endFillWrap when the block element exits.
func (r *renderRunner) beginFillWrap() {
	r.rc.fillWrapBuf = new(bytes.Buffer)
	r.rc.fillWrapWriter = r.rc.w
	r.rc.w = newMarkdownWriter(r.rc.fillWrapBuf)
}

// endFillWrap runs the fill-wrap algorithm on the buffered content and
// writes the wrapped result to the real writer.
func (r *renderRunner) endFillWrap() {
	r.rc.w.FlushLine()
	content := r.rc.fillWrapBuf.String()
	r.rc.w = r.rc.fillWrapWriter
	r.rc.fillWrapBuf = nil
	r.rc.fillWrapWriter = nil

	content = strings.TrimRight(content, "\n")
	if len(content) == 0 {
		return
	}

	prefixWidth := r.rc.w.PrefixWidth()
	wrapped := fillWrap(content, r.rc.config.PrintWidth, prefixWidth)
	r.rc.w.WriteBytes([]byte(wrapped))
}

// inFillWrap reports whether inline content is currently being buffered
// for fill-wrapping.
func (r *renderRunner) inFillWrap() bool {
	return r.rc.fillWrapBuf != nil
}

// writeBreakableSpace writes a space that the fill-wrap algorithm may
// convert to a newline. In non-fill-wrap mode or non-breakable context,
// writes a plain space.
func (r *renderRunner) writeBreakableSpace() {
	if r.inFillWrap() && r.rc.singleLineDepth == 0 {
		r.rc.w.WriteByte(fillWrapSentinel)
	} else {
		r.rc.w.WriteByte(' ')
	}
}

// fillWrap implements a greedy line-filling algorithm. It splits text on
// sentinel markers (breakable spaces) and reassembles lines that fit within
// printWidth, accounting for prefix indentation.
func fillWrap(text string, printWidth, prefixWidth int) string {
	if printWidth <= 0 {
		// Unlimited width — just remove sentinels.
		return strings.ReplaceAll(text, string(rune(fillWrapSentinel)), " ")
	}

	available := printWidth - prefixWidth
	if available <= 0 {
		return strings.ReplaceAll(text, string(rune(fillWrapSentinel)), " ")
	}

	parts := strings.Split(text, string(rune(fillWrapSentinel)))
	if len(parts) <= 1 {
		return text
	}

	var result strings.Builder
	lineWidth := 0

	for i, part := range parts {
		if i == 0 {
			result.WriteString(part)
			lineWidth = displayWidthAfterLastLineBreak(part)
			continue
		}

		partWidth := displayWidthBeforeFirstLineBreak(part)
		if lineWidth+1+partWidth > available {
			result.WriteByte('\n')
			result.WriteString(part)
			lineWidth = displayWidthAfterLastLineBreak(part)
		} else {
			result.WriteByte(' ')
			result.WriteString(part)
			if strings.ContainsRune(part, '\n') {
				lineWidth = displayWidthAfterLastLineBreak(part)
			} else {
				lineWidth += 1 + partWidth
			}
		}
	}

	return result.String()
}

// displayWidth returns the display width of a string, counting CJK
// characters as double-width.
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if isCJKRange(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func displayWidthBeforeFirstLineBreak(s string) int {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return displayWidth(s[:idx])
	}
	return displayWidth(s)
}

func displayWidthAfterLastLineBreak(s string) int {
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		return displayWidth(s[idx+1:])
	}
	return displayWidth(s)
}

// markBreakableSpaces replaces breakable spaces in text with the fill-wrap
// sentinel. A space is not breakable if it's between CJ characters or if
// the word after it would create block-level syntax when starting a line.
func markBreakableSpaces(text []byte) []byte {
	if !bytes.ContainsRune(text, ' ') {
		return text
	}

	runes := []rune(string(text))
	var result []byte
	for i, r := range runes {
		if r != ' ' {
			result = append(result, string(r)...)
			continue
		}

		// Check if this space is breakable based on surrounding characters.
		var prev, next rune
		if i > 0 {
			prev = runes[i-1]
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		breakable := isBreakableSpaceContext(prev, next)

		// Check if the word after this space would create block syntax.
		if breakable {
			nextWord := extractNextWord(runes, i+1)
			if isSyntaxUnsafeWord(nextWord) {
				breakable = false
			}
		}

		if breakable {
			result = append(result, fillWrapSentinel)
		} else {
			result = append(result, ' ')
		}
	}
	return result
}

// extractNextWord returns the run of non-space characters starting at pos.
func extractNextWord(runes []rune, pos int) string {
	end := pos
	for end < len(runes) && runes[end] != ' ' {
		end++
	}
	return string(runes[pos:end])
}

// isSyntaxUnsafeWord reports whether a word would create block-level
// markdown syntax if it appeared at the start of a line. Matches prettier's
// regex: /^>|^(?:[*+-]|#{1,6}|\d+[).])$/
func isSyntaxUnsafeWord(word string) bool {
	if len(word) == 0 {
		return false
	}

	// Starts with ">" — could create a blockquote.
	if word[0] == '>' {
		return true
	}

	// Exact match: single list marker.
	if len(word) == 1 && (word[0] == '*' || word[0] == '+' || word[0] == '-') {
		return true
	}

	// Exact match: 1-6 hash characters.
	if len(word) >= 1 && len(word) <= 6 {
		allHash := true
		for i := range len(word) {
			if word[i] != '#' {
				allHash = false
				break
			}
		}
		if allHash {
			return true
		}
	}

	// Exact match: digits followed by "." or ")".
	if word[0] >= '0' && word[0] <= '9' {
		i := 0
		for i < len(word) && word[i] >= '0' && word[i] <= '9' {
			i++
		}
		if i < len(word) && i == len(word)-1 && (word[i] == '.' || word[i] == ')') {
			return true
		}
	}

	return false
}

// isBreakableSpaceContext reports whether a space between the preceding and
// following runes is a valid break point for fill-wrapping. Matches
// prettier's isBreakable logic: CJ-adjacent spaces are not breakable.
func isBreakableSpaceContext(preceding, following rune) bool {
	if preceding == 0 || following == 0 {
		return true
	}

	prevKind := ClassifyRune(preceding)
	nextKind := ClassifyRune(following)

	// Korean ↔ CJ: breakable.
	if (prevKind == KindKLetter && nextKind == KindCJLetter) ||
		(prevKind == KindCJLetter && nextKind == KindKLetter) {
		return true
	}

	// CJ adjacent (either side): not breakable.
	if prevKind == KindCJLetter || nextKind == KindCJLetter {
		return false
	}

	return true
}

// --- Helpers ---

// handleIgnoredNode checks whether a block node falls within a
// prettier-ignore-start/end range. If it is the start comment, the entire
// range is rendered verbatim from source. If it is any other node in the
// range, it is skipped. Returns true if the node was handled.
func (r *renderRunner) handleIgnoredNode(node ast.Node, entering bool) (ast.WalkStatus, bool) {
	if _, ignored := r.rc.ignoredNextNodes[node]; ignored {
		if entering {
			r.writeBlockSeparator(node)
			r.renderNodeSource(node)
		}
		return ast.WalkSkipChildren, true
	}

	if entering {
		// Check if this node starts an ignore range.
		for _, ir := range r.rc.ignoreRanges {
			if node == ir.startNode {
				r.writeBlockSeparator(node)
				// Write start comment verbatim from source.
				r.renderLines(node)
				// Write raw source between start and end comments.
				r.writeRawBytes(r.rc.source[ir.betweenStart:ir.betweenEnd])
				// Write end comment verbatim from source.
				r.renderLines(ir.endNode)
				r.rc.w.FlushLine()
				return ast.WalkSkipChildren, true
			}
		}
	}

	// Check if this node is inside an ignore range (or is the end comment).
	if _, ignored := r.rc.ignoredNodes[node]; ignored {
		return ast.WalkSkipChildren, true
	}

	return 0, false
}

// writeBlockSeparator writes a blank line before a block element when needed.
// This implements prettier's block spacing rules from children.js.
func (r *renderRunner) writeBlockSeparator(node ast.Node) {
	prev := node.PreviousSibling()
	if prev == nil {
		return
	}

	// prettier-ignore: suppress blank line after ignore comment.
	if isPrettierIgnoreComment(prev, r.rc.source) {
		return
	}

	parent := node.Parent()
	if parent == nil {
		return
	}

	// Inside a ListItem, apply special rules:
	if parent.Kind() == ast.KindListItem {
		// Nested lists inside tight list items do not get blank lines.
		if node.Kind() == ast.KindList {
			if grandparent := parent.Parent(); grandparent != nil {
				if list, ok := grandparent.(*ast.List); ok && !list.IsTight && hasBlankPreviousLines(node) {
					r.rc.w.EndLine()
				}
			}
			return
		}
		// Other children in a list item get blank lines only in loose lists.
		if grandparent := parent.Parent(); grandparent != nil {
			if list, ok := grandparent.(*ast.List); ok {
				if !list.IsTight {
					r.rc.w.EndLine()
				}
				return
			}
		}
		return
	}

	// goldmark's HasBlankPreviousLines covers most block separator cases.
	if hasBlankPreviousLines(node) {
		r.rc.w.EndLine()
		return
	}

	// Block elements in Document and Blockquote always get blank line separation.
	switch parent.Kind() {
	case ast.KindDocument, ast.KindBlockquote:
		r.rc.w.EndLine()
	}
}

// prettierIgnoreKind returns the kind of prettier-ignore directive for an HTML
// block: "next" for <!-- prettier-ignore -->, "start" for
// <!-- prettier-ignore-start -->, "end" for <!-- prettier-ignore-end -->,
// or "" if none.
func prettierIgnoreKind(node ast.Node, source []byte) string {
	if node.Kind() != ast.KindHTMLBlock {
		return ""
	}
	lines := sourceSegmentsOf(node)
	if htmlBlock, ok := node.(*ast.HTMLBlock); ok {
		lines = htmlBlock.Value.Segments()
	}
	if lines.Len() == 0 {
		return ""
	}
	var text []byte
	for i := range lines.Len() {
		seg := lines.At(i)
		text = append(text, seg.Bytes(source)...)
	}
	trimmed := string(bytes.TrimSpace(text))
	switch trimmed {
	case "<!-- prettier-ignore -->":
		return "next"
	case "<!-- prettier-ignore-start -->":
		return "start"
	case "<!-- prettier-ignore-end -->":
		return "end"
	}
	return ""
}

// isPrettierIgnoreComment returns true if the node is a <!-- prettier-ignore --> comment.
func isPrettierIgnoreComment(node ast.Node, source []byte) bool {
	return prettierIgnoreKind(node, source) == "next"
}

// writeURL writes a link/image URL. If the URL contains spaces or characters
// that are dangerous inside the `[text](url)` syntax, it is wrapped in <>.
func (r *renderRunner) writeURL(url []byte, dangerousChars string) {
	urlStr := string(url)
	needsAngleBrackets := strings.ContainsAny(urlStr, " "+dangerousChars)
	if needsAngleBrackets {
		// Encode < and > inside angle-bracket URLs.
		encoded := strings.NewReplacer("<", "%3C", ">", "%3E").Replace(urlStr)
		r.rc.w.WriteBytes([]byte("<" + encoded + ">"))
	} else {
		r.rc.w.WriteBytes(url)
	}
}

// writeLinkTitle writes a link/image title in the configured quote style.
func (r *renderRunner) writeLinkTitle(title []byte) {
	if len(title) == 0 {
		return
	}
	titleStr := string(title)
	q := byte('"')
	if r.rc.config.SingleQuote {
		q = '\''
	}
	// If title contains both quote types but not ")", use parens.
	if strings.ContainsRune(titleStr, '"') && strings.ContainsRune(titleStr, '\'') && !strings.ContainsRune(titleStr, ')') {
		r.rc.w.WriteBytes([]byte(" (" + titleStr + ")"))
		return
	}
	escaped := strings.ReplaceAll(titleStr, string(q), `\`+string(q))
	r.rc.w.WriteBytes([]byte(" " + string(q) + escaped + string(q)))
}

// writeRawBytes writes source bytes without trimming trailing line whitespace.
func (r *renderRunner) writeRawBytes(b []byte) {
	r.rc.w.PushPreserveTrailingWhitespace()
	r.rc.w.WriteBytes(b)
	r.rc.w.PopPreserveTrailingWhitespace()
}

func (r *renderRunner) flushRawLine() {
	r.rc.w.PushPreserveTrailingWhitespace()
	r.rc.w.FlushLine()
	r.rc.w.PopPreserveTrailingWhitespace()
}

// renderLines writes the line segments of a block node verbatim.
func (r *renderRunner) renderLines(node ast.Node) {
	r.rc.w.PushPreserveTrailingWhitespace()
	defer r.rc.w.PopPreserveTrailingWhitespace()

	lines := sourceSegmentsOf(node)
	if htmlBlock, ok := node.(*ast.HTMLBlock); ok {
		lines = htmlBlock.Value.Segments()
	}
	for i := range lines.Len() {
		seg := lines.At(i)
		val := seg.Bytes(r.rc.source)
		r.rc.w.WriteBytes(val)
		r.rc.w.FlushLine()
	}
}

// renderNodeSource writes the original source for a block node.
func (r *renderRunner) renderNodeSource(node ast.Node) {
	start, end, ok := nodeSourceRange(node, r.rc.source)
	if !ok {
		return
	}
	r.writeRawBytes(r.rc.source[start:end])
	r.flushRawLine()
}

func nodeSourceRange(node ast.Node, source []byte) (int, int, bool) {
	start, end, ok := nodeLineRange(node)
	if !ok {
		return 0, 0, false
	}
	start = lineStartOffset(source, start)
	return start, end, true
}

func nodeLineRange(node ast.Node) (int, int, bool) {
	ok := false
	var start, end int
	lines := sourceSegmentsOf(node)
	if htmlBlock, ok := node.(*ast.HTMLBlock); ok {
		lines = htmlBlock.Value.Segments()
	}
	if codeBlock, ok := node.(*ast.CodeBlock); ok {
		lines = codeBlock.Value.Segments()
		if !codeBlock.Info.IsEmpty() {
			seg := codeBlock.Info.Index()
			if !ok || seg.Start < start {
				start = seg.Start
			}
			if !ok || seg.Stop > end {
				end = seg.Stop
			}
			ok = true
		}
	}
	for _, seg := range lines {
		if !ok || seg.Start < start {
			start = seg.Start
		}
		if !ok || seg.Stop > end {
			end = seg.Stop
		}
		ok = true
	}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		childStart, childEnd, childOK := nodeLineRange(child)
		if !childOK {
			continue
		}
		if !ok || childStart < start {
			start = childStart
		}
		if !ok || childEnd > end {
			end = childEnd
		}
		ok = true
	}

	return start, end, ok
}

func lineStartOffset(source []byte, pos int) int {
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

// nthListSiblingIndex counts the index of the given list node among
// consecutive same-type list siblings of the parent. This drives
// marker alternation (- vs * for unordered, . vs ) for ordered).
func nthListSiblingIndex(list *ast.List, parent ast.Node) int {
	if parent == nil {
		return 0
	}
	idx := -1
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		if l, ok := c.(*ast.List); ok && l.IsOrdered() == list.IsOrdered() {
			idx++
		} else {
			idx = -1
		}
		if c == list {
			if idx < 0 {
				return 0
			}
			return idx
		}
	}
	return 0
}

func nthUnorderedListMarkerRunIndex(list *ast.List, parent ast.Node, source []byte) int {
	if parent == nil {
		return 0
	}
	idx := -1
	var currentMarker byte
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		l, ok := c.(*ast.List)
		if !ok || l.IsOrdered() {
			idx = -1
			currentMarker = 0
			continue
		}
		marker := sourceUnorderedListMarker(l, source)
		if idx < 0 || marker != currentMarker {
			idx++
			currentMarker = marker
		}
		if c == list {
			return idx
		}
	}
	return 0
}

func nthUnorderedListItemMarkerRunIndex(list *ast.List, item ast.Node, source []byte) int {
	idx := nthUnorderedListMarkerRunIndex(list, list.Parent(), source)
	var currentMarker byte
	for c := list.FirstChild(); c != nil; c = c.NextSibling() {
		marker := sourceUnorderedListItemMarker(c, source)
		if currentMarker == 0 {
			currentMarker = marker
		} else if marker != currentMarker {
			idx++
			currentMarker = marker
		}
		if c == item {
			return idx
		}
	}
	return idx
}

func sourceUnorderedListMarker(list *ast.List, source []byte) byte {
	if list == nil || list.IsOrdered() {
		return 0
	}
	return sourceUnorderedListItemMarker(list.FirstChild(), source)
}

func sourceUnorderedListItemMarker(item ast.Node, source []byte) byte {
	lineStart := sourceListItemLineStart(item, source)
	if lineStart < 0 {
		return 0
	}
	for pos := lineStart; pos < len(source); pos++ {
		switch source[pos] {
		case ' ', '\t', '>':
			continue
		case '-', '*', '+':
			return source[pos]
		default:
			return 0
		}
	}
	return 0
}

// isGitDiffFriendlyOrderedList detects ordered lists that use the pattern
// "0. 1. 1. ..." or "N. 1. 1. ..." for cleaner git diffs. The key signal
// is that the second item's number is 1 in the source.
func isGitDiffFriendlyOrderedList(list *ast.List, source []byte) bool {
	if !list.IsOrdered() || list.ChildCount() < 2 {
		return false
	}
	secondItem := list.FirstChild().NextSibling()
	secondNum := sourceListItemNumber(secondItem, source)
	if secondNum != 1 {
		return false
	}
	firstNum := sourceListItemNumber(list.FirstChild(), source)
	if firstNum != 0 {
		return true
	}
	// If first is 0 and second is 1, need a third item also being 1 to confirm.
	if list.ChildCount() > 2 {
		thirdItem := secondItem.NextSibling()
		return sourceListItemNumber(thirdItem, source) == 1
	}
	return false
}

// sourceListItemNumber extracts the number from a list item's source text.
func sourceListItemNumber(item ast.Node, source []byte) int {
	if item == nil {
		return -1
	}
	start := sourceListItemLineStart(item, source)
	if start < 0 {
		return -1
	}
	// Parse the number from the line.
	num := 0
	pos := start
	// Skip leading whitespace.
	for pos < len(source) && source[pos] == ' ' {
		pos++
	}
	// Skip blockquote markers.
	for pos < len(source) && (source[pos] == '>' || source[pos] == ' ') {
		if source[pos] == '>' {
			pos++
			if pos < len(source) && source[pos] == ' ' {
				pos++
			}
		} else {
			pos++
		}
	}
	found := false
	for pos < len(source) && source[pos] >= '0' && source[pos] <= '9' {
		num = num*10 + int(source[pos]-'0')
		pos++
		found = true
	}
	if !found {
		return -1
	}
	return num
}

func sourceListItemLineStart(item ast.Node, source []byte) int {
	if item == nil {
		return -1
	}
	// Find the start of the list item in the source by looking at its first
	// child's lines or the item's own position.
	lines := sourceSegmentsOf(item)
	var start int
	if lines.Len() > 0 {
		start = lines.At(0).Start
	} else if item.FirstChild() != nil {
		childLines := sourceSegmentsOf(item.FirstChild())
		if childLines.Len() > 0 {
			start = childLines.At(0).Start
		} else {
			return -1
		}
	} else {
		return -1
	}
	// Scan backwards to find the line start.
	for start > 0 && source[start-1] != '\n' {
		start--
	}
	return start
}

// isAlignedOrderedList determines if an ordered list should have its prefixes
// aligned to tabWidth boundaries. Mirrors prettier's markAlignedList logic.
func isAlignedOrderedList(list *ast.List, source []byte, tabWidth int) bool {
	if !list.IsOrdered() || list.ChildCount() == 0 {
		return false
	}
	// Check if any ancestor list is NOT aligned — if so, neither is this one.
	// (We don't track this currently, so skip for now.)

	first := list.FirstChild()
	firstStart := listItemContentColumn(first, source)
	if firstStart < 0 {
		return false
	}
	if list.ChildCount() == 1 {
		return firstStart%tabWidth == 0
	}
	second := first.NextSibling()
	secondStart := listItemContentColumn(second, source)
	if firstStart != secondStart {
		return false
	}
	return firstStart%tabWidth == 0
}

// listItemContentColumn returns the 0-based column of the first content
// character in a list item (after the marker and spaces).
func listItemContentColumn(item ast.Node, source []byte) int {
	if item == nil || item.FirstChild() == nil {
		return -1
	}
	child := item.FirstChild()
	childLines := sourceSegmentsOf(child)
	if childLines.Len() == 0 {
		// Inline content — check first text child.
		if first := child.FirstChild(); first != nil {
			if t, ok := first.(*ast.Text); ok {
				seg := t.Value.Index()
				col := seg.Start
				// Find column relative to line start.
				for col > 0 && source[col-1] != '\n' {
					col--
				}
				return seg.Start - col
			}
		}
		return -1
	}
	seg := childLines.At(0)
	col := seg.Start
	for col > 0 && source[col-1] != '\n' {
		col--
	}
	return seg.Start - col
}

// alignListPrefix pads an ordered list prefix to align to tabWidth boundaries.
// E.g., with tabWidth=4: "1. " (3 chars) → "1.  " (4 chars).
func alignListPrefix(prefix []byte, tabWidth int) []byte {
	rest := len(prefix) % tabWidth
	if rest == 0 {
		return prefix
	}
	additional := tabWidth - rest
	if additional >= 4 {
		// 4+ trailing spaces would trigger indented code block interpretation.
		return prefix
	}
	return append(prefix, bytes.Repeat([]byte{' '}, additional)...)
}

// maxContinuousCount returns the length of the longest run of ch in s.
func maxContinuousCount(s string, ch byte) int {
	maxCount := 0
	count := 0
	for i := range len(s) {
		if s[i] == ch {
			count++
			if count > maxCount {
				maxCount = count
			}
		} else {
			count = 0
		}
	}
	return maxCount
}

// minNotPresentContinuousCount returns the smallest positive integer n such
// that a run of n consecutive ch characters does not appear in s.
func minNotPresentContinuousCount(s string, ch byte) int {
	var runs []int
	count := 0
	for i := range len(s) {
		if s[i] == ch {
			count++
		} else if count > 0 {
			runs = append(runs, count)
			count = 0
		}
	}
	if count > 0 {
		runs = append(runs, count)
	}
	for n := 1; n <= len(s)+1; n++ {
		if !slices.Contains(runs, n) {
			return n
		}
	}
	return 1
}
