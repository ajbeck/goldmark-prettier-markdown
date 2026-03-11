package prettier

import "github.com/yuin/goldmark/renderer"

// ProseWrap controls how prose text is wrapped.
type ProseWrap int

const (
	// ProseWrapPreserve preserves original line breaks.
	ProseWrapPreserve ProseWrap = iota
	// ProseWrapAlways wraps prose to printWidth. Not yet implemented.
	ProseWrapAlways
	// ProseWrapNever collapses prose to single lines. Soft line breaks become
	// spaces (or empty for CJ-to-CJ transitions). Tables use compact mode
	// when the aligned version exceeds printWidth.
	ProseWrapNever
)

// Config holds configuration for the prettier markdown renderer.
type Config struct {
	ProseWrap   ProseWrap
	SingleQuote bool
	TabWidth    int
	PrintWidth  int
}

// DefaultConfig returns a Config with prettier's default values.
func DefaultConfig() Config {
	return Config{
		ProseWrap:   ProseWrapPreserve,
		SingleQuote: false,
		TabWidth:    2,
		PrintWidth:  80,
	}
}

// Option configures the prettier markdown renderer.
type Option interface {
	SetPrettierOption(*Config)
}

type withProseWrap struct{ value ProseWrap }

func (o withProseWrap) SetPrettierOption(c *Config) { c.ProseWrap = o.value }

// WithProseWrap sets how prose text is wrapped.
func WithProseWrap(v ProseWrap) Option { return withProseWrap{v} }

type withSingleQuote struct{ value bool }

func (o withSingleQuote) SetPrettierOption(c *Config) { c.SingleQuote = o.value }

// WithSingleQuote sets whether to use single quotes for link/image titles.
func WithSingleQuote(v bool) Option { return withSingleQuote{v} }

type withTabWidth struct{ value int }

func (o withTabWidth) SetPrettierOption(c *Config) { c.TabWidth = o.value }

// WithTabWidth sets the tab width used for list alignment.
func WithTabWidth(v int) Option { return withTabWidth{v} }

type withPrintWidth struct{ value int }

func (o withPrintWidth) SetPrettierOption(c *Config) { c.PrintWidth = o.value }

// WithPrintWidth sets the target line width for prose wrapping and compact tables.
func WithPrintWidth(v int) Option { return withPrintWidth{v} }

// optionName is the renderer.OptionName for prettier config options passed
// through goldmark's renderer.Option interface.
const optionName renderer.OptionName = "PrettierConfig"
