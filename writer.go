package prettier

import (
	"bytes"
	"io"
	"unicode"

	"github.com/yuin/goldmark/util"
)

// lineDelim is the newline character used as line delimiter.
const lineDelim = '\n'

// linePrefix associates a byte prefix with a line range.
type linePrefix struct {
	// startLine is the first line this prefix applies to (inclusive).
	startLine int
	// endLine is the last line this prefix applies to (inclusive).
	// -1 means "all subsequent lines".
	endLine int
	// prefix is the bytes to prepend.
	prefix []byte
}

// markdownWriter buffers output line-by-line, applies line prefixes (for
// blockquotes and list indentation), and trims trailing whitespace.
type markdownWriter struct {
	buf      bytes.Buffer
	output   io.Writer
	prefixes []linePrefix
	line     int
	err      error
}

var _ util.BufWriter = (*markdownWriter)(nil)

func newMarkdownWriter(w io.Writer) *markdownWriter {
	return &markdownWriter{output: w}
}

// Reset clears all state and switches to a new underlying writer.
func (m *markdownWriter) Reset(w io.Writer) {
	m.buf.Reset()
	m.output = w
	m.prefixes = m.prefixes[:0]
	m.line = 0
	m.err = nil
}

// PushPrefix adds a line prefix. By default it applies from the current line
// onward. Optional arguments: lineRanges[0] is a start offset relative to the
// current line; lineRanges[1] is a duration relative to the start.
func (m *markdownWriter) PushPrefix(prefix []byte, lineRanges ...int) {
	lp := linePrefix{
		endLine: -1,
		prefix:  prefix,
	}
	if len(lineRanges) > 0 {
		lp.startLine = m.line + lineRanges[0]
		if len(lineRanges) > 1 {
			lp.endLine = lp.startLine + lineRanges[1]
		}
	}
	m.prefixes = append(m.prefixes, lp)
}

// PrefixWidth returns the total width of all prefixes active on the current line.
func (m *markdownWriter) PrefixWidth() int {
	width := 0
	for _, lp := range m.prefixes {
		if lp.startLine <= m.line && (lp.endLine == -1 || m.line <= lp.endLine) {
			width += len(lp.prefix)
		}
	}
	return width
}

// PopPrefix removes the most recently pushed prefix.
func (m *markdownWriter) PopPrefix() {
	m.prefixes = m.prefixes[:len(m.prefixes)-1]
}

// FlushLine ends the current line if the buffer is non-empty.
func (m *markdownWriter) FlushLine() {
	if m.buf.Len() > 0 {
		m.EndLine()
	}
}

// EndLine ends the current line unconditionally by writing a newline.
func (m *markdownWriter) EndLine() {
	m.WriteBytes([]byte{lineDelim})
}

// WriteBytes writes data to the internal buffer and flushes any complete lines
// (delimited by newline) to the underlying writer with prefixes applied and
// trailing whitespace trimmed.
func (m *markdownWriter) WriteBytes(data []byte) int {
	if m.err != nil {
		return 0
	}
	n, _ := m.buf.Write(data)

	var prefixed bytes.Buffer
	for bytes.ContainsRune(m.buf.Bytes(), lineDelim) {
		line, _ := m.buf.ReadBytes(lineDelim)

		// Build prefix for this line.
		for _, lp := range m.prefixes {
			if lp.startLine <= m.line && (lp.endLine == -1 || m.line <= lp.endLine) {
				prefixed.Write(lp.prefix)
			}
		}
		prefixed.Write(line)

		// Trim trailing whitespace, then re-add the newline.
		trimmed := bytes.TrimRightFunc(prefixed.Bytes(), unicode.IsSpace)
		prefixed.Truncate(len(trimmed))
		prefixed.WriteByte(lineDelim)

		if _, err := m.output.Write(prefixed.Bytes()); err != nil {
			m.err = err
			return 0
		}
		m.line++
		prefixed.Reset()
	}
	return n
}

// Err returns the last write error, or nil.
func (m *markdownWriter) Err() error { return m.err }

// --- util.BufWriter interface implementation ---

func (m *markdownWriter) Write(data []byte) (int, error) {
	return m.WriteBytes(data), m.err
}

func (m *markdownWriter) Available() int { return m.buf.Available() }

func (m *markdownWriter) Buffered() int { return m.buf.Len() }

func (m *markdownWriter) Flush() error {
	m.FlushLine()
	return nil
}

func (m *markdownWriter) WriteByte(c byte) error {
	m.WriteBytes([]byte{c})
	return m.err
}

func (m *markdownWriter) WriteRune(r rune) (int, error) {
	var buf [4]byte
	n := copy(buf[:], string(r))
	return m.WriteBytes(buf[:n]), m.err
}

func (m *markdownWriter) WriteString(s string) (int, error) {
	return m.WriteBytes([]byte(s)), m.err
}
