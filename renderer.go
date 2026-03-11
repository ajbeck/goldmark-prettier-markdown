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

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Renderer renders goldmark AST nodes as prettier-formatted markdown.
type Renderer struct {
	config Config
	rc     *renderContext
}

var _ renderer.NodeRenderer = (*Renderer)(nil)

// NewRenderer returns a new Renderer with the given options.
func NewRenderer(opts ...Option) *Renderer {
	cfg := DefaultConfig()
	for _, o := range opts {
		o.SetPrettierOption(&cfg)
	}
	return &Renderer{config: cfg}
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r *Renderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Block nodes
	reg.Register(ast.KindDocument, r.renderDocument)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindTextBlock, r.renderTextBlock)
	reg.Register(ast.KindThematicBreak, r.renderThematicBreak)

	// Inline nodes
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
	reg.Register(ast.KindEmphasis, r.renderEmphasis)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindRawHTML, r.renderRawHTML)
	reg.Register(ast.KindText, r.renderText)
	reg.Register(ast.KindString, r.renderString)

	// GFM extension nodes
	reg.Register(east.KindTable, r.renderTable)
	reg.Register(east.KindTableHeader, r.renderTableHeader)
	reg.Register(east.KindTableRow, r.renderTableRow)
	reg.Register(east.KindTableCell, r.renderTableCell)
	reg.Register(east.KindStrikethrough, r.renderStrikethrough)
	reg.Register(east.KindTaskCheckBox, r.renderTaskCheckBox)
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

// --- Block node renderers ---

func (r *Renderer) renderDocument(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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
func (r *Renderer) scanIgnoreRanges(doc ast.Node) {
	var startNode ast.Node
	var startEnd int // byte offset at end of start comment

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		kind := prettierIgnoreKind(child, r.rc.source)
		switch kind {
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
	lines := node.Lines()
	if lines.Len() == 0 {
		return 0
	}
	return lines.At(0).Start
}

// htmlBlockEndOffset returns the byte offset after the last character of an HTML block,
// including the closure line if present.
func htmlBlockEndOffset(node ast.Node) int {
	if hb, ok := node.(*ast.HTMLBlock); ok && hb.HasClosure() {
		return hb.ClosureLine.Stop
	}
	lines := node.Lines()
	if lines.Len() == 0 {
		return 0
	}
	return lines.At(lines.Len() - 1).Stop
}

func (r *Renderer) renderParagraph(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func (r *Renderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.Heading)
	isSetext := r.isSetextHeading(n)
	if entering {
		r.writeBlockSeparator(node)
		if isSetext {
			if r.rc.config.ProseWrap == ProseWrapAlways {
				r.beginFillWrap()
			}
			return r.renderSetextHeadingEnter(n)
		}
		r.rc.w.WriteBytes(bytes.Repeat([]byte("#"), n.Level))
		if n.HasChildren() {
			r.rc.w.WriteBytes([]byte(" "))
		}
	} else {
		if isSetext {
			if r.rc.config.ProseWrap == ProseWrapAlways {
				r.endFillWrap()
			}
			r.renderSetextHeadingExit(n)
		}
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) isSetextHeading(n *ast.Heading) bool {
	if n.Level > 2 || n.Lines().Len() == 0 {
		return false
	}
	// Check if an underline ('=' or '-') follows the last content line in the
	// source. This is the definitive signal that goldmark parsed it as setext.
	lastSeg := n.Lines().At(n.Lines().Len() - 1)
	pos := lastSeg.Stop
	// Must have exactly one newline (possibly with \r) — no blank lines allowed.
	if pos < len(r.rc.source) && r.rc.source[pos] == '\r' {
		pos++
	}
	if pos >= len(r.rc.source) || r.rc.source[pos] != '\n' {
		return false
	}
	pos++ // skip the single newline
	// Skip blockquote markers and leading spaces on the underline.
	for pos < len(r.rc.source) && (r.rc.source[pos] == '>' || r.rc.source[pos] == ' ') {
		pos++
	}
	if pos >= len(r.rc.source) {
		return false
	}
	// The underline character must match the heading level and the rest of the
	// line must contain only that character (plus optional trailing whitespace).
	ch := r.rc.source[pos]
	if !((n.Level == 1 && ch == '=') || (n.Level == 2 && ch == '-')) {
		return false
	}
	for pos < len(r.rc.source) && r.rc.source[pos] == ch {
		pos++
	}
	// Remaining characters on the line must be whitespace or EOL.
	for pos < len(r.rc.source) && r.rc.source[pos] == ' ' {
		pos++
	}
	return pos >= len(r.rc.source) || r.rc.source[pos] == '\n' || r.rc.source[pos] == '\r'
}

func (r *Renderer) renderSetextHeadingEnter(n *ast.Heading) (ast.WalkStatus, error) {
	// Setext heading content is rendered by child inline nodes — just continue.
	return ast.WalkContinue, nil
}

func (r *Renderer) renderSetextHeadingExit(n *ast.Heading) {
	// Find the underline in the source after the last content line.
	lastSeg := n.Lines().At(n.Lines().Len() - 1)
	pos := lastSeg.Stop
	// Skip past newline/CR after content.
	for pos < len(r.rc.source) && (r.rc.source[pos] == '\n' || r.rc.source[pos] == '\r') {
		pos++
	}
	// Skip blockquote markers and leading whitespace on the underline.
	for pos < len(r.rc.source) && (r.rc.source[pos] == '>' || r.rc.source[pos] == ' ') {
		pos++
	}
	// Read the underline characters ('=' or '-').
	underlineStart := pos
	if pos < len(r.rc.source) {
		ch := r.rc.source[pos]
		for pos < len(r.rc.source) && r.rc.source[pos] == ch {
			pos++
		}
	}
	underline := r.rc.source[underlineStart:pos]
	r.rc.w.WriteBytes([]byte("\n"))
	r.rc.w.WriteBytes(underline)
}

func (r *Renderer) renderBlockquote(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func (r *Renderer) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if entering {
		r.writeBlockSeparator(node)
		// Extra blank line when indented code follows a list (prettier: shouldPrePrintTripleHardline).
		// Without this, CommonMark parsers treat the indented content as list continuation.
		if prev := node.PreviousSibling(); prev != nil && prev.Kind() == ast.KindList {
			r.rc.w.EndLine()
		}
		r.rc.w.PushPrefix([]byte("    "))
		r.renderLines(node)
	} else {
		r.rc.w.PopPrefix()
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.FencedCodeBlock)
	if entering {
		r.writeBlockSeparator(node)

		// Determine fence length: at least 3, more if content contains backticks.
		fenceLen := 3
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			line := string(seg.Value(source))
			if cnt := maxContinuousCount(line, '`'); cnt >= fenceLen {
				fenceLen = cnt + 1
			}
		}

		fence := strings.Repeat("`", fenceLen)
		r.rc.w.WriteBytes([]byte(fence))
		if info := n.Info; info != nil {
			// Info contains the full info string (language + optional metadata).
			r.rc.w.WriteBytes(info.Value(source))
		}
		r.rc.w.EndLine()
		r.renderLines(node)
		r.rc.w.WriteBytes([]byte(fence))
	} else {
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	n := node.(*ast.HTMLBlock)
	if entering {
		r.writeBlockSeparator(node)
		r.renderLines(node)
	} else {
		if n.HasClosure() {
			cl := n.ClosureLine
			r.rc.w.WriteBytes(cl.Value(source))
			r.rc.w.FlushLine()
		}
		r.rc.w.FlushLine()
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func (r *Renderer) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.writeBlockSeparator(node)
		lc := &r.rc.listStack[len(r.rc.listStack)-1]
		nthIdx := nthListSiblingIndex(lc.list, node.Parent())

		var prefix []byte
		if lc.list.IsOrdered() {
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

func (r *Renderer) renderTextBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func (r *Renderer) renderThematicBreak(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
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

func (r *Renderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	text := n.Value(source)

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
func (r *Renderer) writeSoftLineBreakNever(text []byte, n *ast.Text) {
	// Get the last rune of the current text.
	preceding, _ := utf8.DecodeLastRune(text)

	// Get the first rune of the next sibling's text.
	var following rune
	if next := n.NextSibling(); next != nil {
		if nextText, ok := next.(*ast.Text); ok {
			following, _ = utf8.DecodeRune(nextText.Value(r.rc.source))
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
func (r *Renderer) writeSoftLineBreakAlways(text []byte, n *ast.Text) {
	preceding, _ := utf8.DecodeLastRune(text)

	var following rune
	var nextFirstWord string
	if next := n.NextSibling(); next != nil {
		if nextText, ok := next.(*ast.Text); ok {
			nextVal := nextText.Value(r.rc.source)
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

func (r *Renderer) renderString(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*ast.String)
		r.rc.w.WriteBytes(n.Value)
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderEmphasis(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Emphasis)
	if n.Level == 2 {
		r.rc.w.WriteBytes([]byte("**"))
	} else {
		marker := r.emphasisMarker(n, source)
		r.rc.w.WriteBytes([]byte{marker})
	}
	return ast.WalkContinue, nil
}

// emphasisMarker returns '_' or '*' for a level-1 emphasis node, applying
// prettier's marker selection rules.
func (r *Renderer) emphasisMarker(n *ast.Emphasis, source []byte) byte {
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
		if pe, ok := parent.(*ast.Emphasis); ok && pe.Level == 2 {
			if hasAdjacentWordWithoutPunctuation(pe, source) {
				return '*'
			}
		}
	}

	// Rule 4: nested inside another emphasis.
	for p := n.Parent(); p != nil; p = p.Parent() {
		if pe, ok := p.(*ast.Emphasis); ok && pe.Level == 1 {
			return '*'
		}
	}

	return '_'
}

// originalEmphasisMarker reads the marker character from the source for the
// emphasis node. Falls back to '_'.
func (r *Renderer) originalEmphasisMarker(n *ast.Emphasis, source []byte) byte {
	// Walk inline children to find the position in source.
	if first := n.FirstChild(); first != nil {
		if t, ok := first.(*ast.Text); ok {
			seg := t.Segment
			// The emphasis marker is just before the first child's text.
			if seg.Start > 0 {
				ch := source[seg.Start-1]
				if ch == '*' || ch == '_' {
					return ch
				}
			}
		}
		// For autolinks, the '<' is the first char; marker is before that.
		if _, ok := first.(*ast.AutoLink); ok {
			if first.FirstChild() != nil {
				if t, ok := first.FirstChild().(*ast.Text); ok {
					seg := t.Segment
					// <URL> — the '<' is at seg.Start-1, marker is at seg.Start-2
					if seg.Start >= 2 {
						ch := source[seg.Start-2]
						if ch == '*' || ch == '_' {
							return ch
						}
					}
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
		v := n.Value(source)
		if len(v) > 0 {
			return rune(v[len(v)-1]), true
		}
	case *ast.String:
		if len(n.Value) > 0 {
			return rune(n.Value[len(n.Value)-1]), true
		}
	case *ast.CodeSpan, *ast.Emphasis, *ast.Link, *ast.Image:
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
		v := n.Value(source)
		if len(v) > 0 {
			return rune(v[0]), true
		}
	case *ast.String:
		if len(n.Value) > 0 {
			return rune(n.Value[0]), true
		}
	case *ast.CodeSpan, *ast.Emphasis, *ast.Link, *ast.Image:
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

// isInsideEmphasisOrStrong returns true if the node has an Emphasis ancestor.
func isInsideEmphasisOrStrong(node ast.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.Emphasis); ok {
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

func (r *Renderer) renderCodeSpan(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		// Collect code span content from children.
		var content []byte
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			t := c.(*ast.Text)
			seg := t.Segment
			content = append(content, seg.Value(source)...)
		}

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

func (r *Renderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if entering {
		if r.inFillWrap() {
			r.rc.singleLineDepth++
		}
		r.rc.w.WriteBytes([]byte("["))
	} else {
		r.rc.w.WriteBytes([]byte("]("))
		r.writeURL(n.Destination, ")")
		r.writeLinkTitle(n.Title)
		r.rc.w.WriteBytes([]byte(")"))
		if r.inFillWrap() {
			r.rc.singleLineDepth--
		}
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Image)
	if entering {
		r.rc.w.WriteBytes([]byte("!["))
	} else {
		r.rc.w.WriteBytes([]byte("]("))
		r.writeURL(n.Destination, ")")
		r.writeLinkTitle(n.Title)
		r.rc.w.WriteBytes([]byte(")"))
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.AutoLink)
	if entering {
		r.rc.w.WriteBytes([]byte("<"))
		r.rc.w.WriteBytes(n.URL(source))
	} else {
		r.rc.w.WriteBytes([]byte(">"))
	}
	return ast.WalkContinue, nil
}

func (r *Renderer) renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*ast.RawHTML)
		for i := range n.Segments.Len() {
			seg := n.Segments.At(i)
			r.rc.w.WriteBytes(seg.Value(source))
		}
	}
	return ast.WalkContinue, nil
}

// --- GFM extension node renderers ---

func (r *Renderer) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if status, handled := r.handleIgnoredNode(node, entering); handled {
		return status, nil
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	r.writeBlockSeparator(node)
	table := node.(*east.Table)

	// Pass 1: collect cell text and measure column widths.
	var rows [][]cellInfo
	var colWidths []int

	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		var rowCells []cellInfo
		colIdx := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
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
	r.writeTableRow(rows[0], colWidths, table.Alignments, compact)

	// Alignment row.
	r.writeAlignmentRow(colWidths, table.Alignments, compact)

	// Data rows.
	for _, row := range rows[1:] {
		r.writeTableRow(row, colWidths, table.Alignments, compact)
	}

	return ast.WalkSkipChildren, nil
}

// renderCellContent renders the inline content of a table cell to a string.
func (r *Renderer) renderCellContent(cell ast.Node, source []byte) string {
	var buf bytes.Buffer
	origWriter := r.rc.w
	r.rc.w = newMarkdownWriter(&buf)

	for c := cell.FirstChild(); c != nil; c = c.NextSibling() {
		ast.Walk(c, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			switch n := n.(type) {
			case *ast.Text:
				if entering {
					r.rc.w.WriteBytes(n.Value(source))
				}
			case *ast.String:
				if entering {
					r.rc.w.WriteBytes(n.Value)
				}
			case *ast.Emphasis:
				if n.Level == 2 {
					r.rc.w.WriteBytes([]byte("**"))
				} else {
					marker := r.emphasisMarker(n, source)
					r.rc.w.WriteBytes([]byte{marker})
				}
			case *ast.CodeSpan:
				if entering {
					var content []byte
					for cc := n.FirstChild(); cc != nil; cc = cc.NextSibling() {
						if t, ok := cc.(*ast.Text); ok {
							seg := t.Segment
							content = append(content, seg.Value(source)...)
						}
					}
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
					r.writeURL(n.Destination, ")")
					r.writeLinkTitle(n.Title)
					r.rc.w.WriteBytes([]byte(")"))
				}
			case *ast.Image:
				if entering {
					r.rc.w.WriteBytes([]byte("!["))
				} else {
					r.rc.w.WriteBytes([]byte("]("))
					r.writeURL(n.Destination, ")")
					r.writeLinkTitle(n.Title)
					r.rc.w.WriteBytes([]byte(")"))
				}
			case *ast.AutoLink:
				if entering {
					r.rc.w.WriteBytes([]byte("<"))
					r.rc.w.WriteBytes(n.URL(source))
				} else {
					r.rc.w.WriteBytes([]byte(">"))
				}
			case *ast.RawHTML:
				if entering {
					for i := range n.Segments.Len() {
						seg := n.Segments.At(i)
						r.rc.w.WriteBytes(seg.Value(source))
					}
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

func (r *Renderer) writeTableRow(cells []cellInfo, colWidths []int, alignments []east.Alignment, compact bool) {
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

func (r *Renderer) writeAlignmentRow(colWidths []int, alignments []east.Alignment, compact bool) {
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

func (r *Renderer) renderTableHeader(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *Renderer) renderTableRow(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *Renderer) renderTableCell(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *Renderer) renderStrikethrough(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	r.rc.w.WriteBytes([]byte("~~"))
	return ast.WalkContinue, nil
}

func (r *Renderer) renderTaskCheckBox(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*east.TaskCheckBox)
	if n.IsChecked {
		r.rc.w.WriteBytes([]byte("[x] "))
	} else {
		r.rc.w.WriteBytes([]byte("[ ] "))
	}
	return ast.WalkContinue, nil
}

// --- Fill-wrap for proseWrap "always" ---

// fillWrapSentinel is a zero byte used to mark breakable space positions
// in the fill-wrap buffer. fillWrap splits on this to identify wrap candidates.
const fillWrapSentinel = '\x00'

// beginFillWrap starts capturing inline output into a buffer for later
// fill-wrapping. Call endFillWrap when the block element exits.
func (r *Renderer) beginFillWrap() {
	r.rc.fillWrapBuf = new(bytes.Buffer)
	r.rc.fillWrapWriter = r.rc.w
	r.rc.w = newMarkdownWriter(r.rc.fillWrapBuf)
}

// endFillWrap runs the fill-wrap algorithm on the buffered content and
// writes the wrapped result to the real writer.
func (r *Renderer) endFillWrap() {
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
func (r *Renderer) inFillWrap() bool {
	return r.rc.fillWrapBuf != nil
}

// writeBreakableSpace writes a space that the fill-wrap algorithm may
// convert to a newline. In non-fill-wrap mode or non-breakable context,
// writes a plain space.
func (r *Renderer) writeBreakableSpace() {
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
			lineWidth = displayWidth(part)
			continue
		}

		partWidth := displayWidth(part)
		if lineWidth+1+partWidth > available {
			result.WriteByte('\n')
			result.WriteString(part)
			lineWidth = partWidth
		} else {
			result.WriteByte(' ')
			result.WriteString(part)
			lineWidth += 1 + partWidth
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
func (r *Renderer) handleIgnoredNode(node ast.Node, entering bool) (ast.WalkStatus, bool) {
	if len(r.rc.ignoreRanges) == 0 {
		return 0, false
	}

	// Check if this node starts an ignore range.
	if entering {
		for _, ir := range r.rc.ignoreRanges {
			if node == ir.startNode {
				r.writeBlockSeparator(node)
				// Write start comment verbatim from source.
				r.renderLines(node)
				if hb, ok := node.(*ast.HTMLBlock); ok && hb.HasClosure() {
					r.rc.w.WriteBytes(hb.ClosureLine.Value(r.rc.source))
				}
				// Write raw source between start and end comments.
				r.rc.w.WriteBytes(r.rc.source[ir.betweenStart:ir.betweenEnd])
				// Write end comment verbatim from source.
				r.renderLines(ir.endNode)
				if hb, ok := ir.endNode.(*ast.HTMLBlock); ok && hb.HasClosure() {
					r.rc.w.WriteBytes(hb.ClosureLine.Value(r.rc.source))
				}
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
func (r *Renderer) writeBlockSeparator(node ast.Node) {
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
		// Nested lists inside list items never get blank lines before them
		// (prettier: isInTightListItem when node.type === "list").
		if node.Kind() == ast.KindList {
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
	if node.HasBlankPreviousLines() {
		r.rc.w.EndLine()
		return
	}

	// Block elements in Document or Blockquote always get blank line separation.
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
	lines := node.Lines()
	if lines.Len() == 0 {
		return ""
	}
	var text []byte
	for i := range lines.Len() {
		seg := lines.At(i)
		text = append(text, seg.Value(source)...)
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
func (r *Renderer) writeURL(url []byte, dangerousChars string) {
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
func (r *Renderer) writeLinkTitle(title []byte) {
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

// renderLines writes the line segments of a block node.
func (r *Renderer) renderLines(node ast.Node) {
	lines := node.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		val := seg.Value(r.rc.source)
		r.rc.w.WriteBytes(val)
		r.rc.w.FlushLine()
	}
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
	// Find the start of the list item in the source by looking at its first
	// child's lines or the item's own position.
	lines := item.Lines()
	var start int
	if lines.Len() > 0 {
		start = lines.At(0).Start
	} else if item.FirstChild() != nil {
		childLines := item.FirstChild().Lines()
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
	childLines := child.Lines()
	if childLines.Len() == 0 {
		// Inline content — check first text child.
		if first := child.FirstChild(); first != nil {
			if t, ok := first.(*ast.Text); ok {
				seg := t.Segment
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
