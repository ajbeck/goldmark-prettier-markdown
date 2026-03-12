# goldmark-prettier-markdown

A [goldmark](https://github.com/yuin/goldmark) renderer that outputs
[prettier](https://prettier.io/)-formatted markdown.

Instead of rendering HTML, this renderer takes a goldmark AST and writes it
back as clean, consistently formatted markdown — matching prettier's opinionated
formatting rules.

## Installation

```bash
go get github.com/ajbeck/goldmark-prettier-markdown
```

Requires Go 1.26+.

## Usage

```go
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	prettier "github.com/ajbeck/goldmark-prettier-markdown"
)

func main() {
	r := prettier.NewRenderer()
	md := goldmark.New(
		goldmark.WithRenderer(
			renderer.NewRenderer(
				renderer.WithNodeRenderers(
					util.Prioritized(r, 1000),
				),
			),
		),
	)

	input := []byte("# Hello   World\n\nSome   **bold**  text.\n")
	var buf bytes.Buffer
	if err := md.Convert(input, &buf); err != nil {
		log.Fatal(err)
	}
	fmt.Print(buf.String())
	// Output:
	// # Hello   World
	//
	// Some **bold** text.
}
```

## Options

| Option | Default | Description |
| --- | --- | --- |
| `WithProseWrap(mode)` | `ProseWrapPreserve` | Controls line wrapping: `ProseWrapPreserve`, `ProseWrapAlways`, `ProseWrapNever` |
| `WithPrintWidth(n)` | `80` | Target line width for `ProseWrapAlways` and compact table mode |
| `WithSingleQuote(bool)` | `false` | Use single quotes for link/image titles |
| `WithTabWidth(n)` | `2` | Tab width for list indentation alignment |

```go
r := prettier.NewRenderer(
	prettier.WithProseWrap(prettier.ProseWrapAlways),
	prettier.WithPrintWidth(80),
)
```

### Prose Wrap Modes

- **`ProseWrapPreserve`** — Original line breaks in prose are kept as-is.
- **`ProseWrapAlways`** — Prose is re-wrapped to fit within `PrintWidth`. Respects CJK character joining rules.
- **`ProseWrapNever`** — Soft line breaks are collapsed into spaces. Tables use compact mode when they exceed `PrintWidth`.

## Supported Node Types

### CommonMark

Headings (ATX and setext), paragraphs, blockquotes, ordered and unordered lists,
list items, fenced and indented code blocks, thematic breaks, inline code,
emphasis, strong, links, autolinks, images, hard breaks, raw HTML.

### GFM Extensions

Tables (with column alignment and compact mode), strikethrough, task checkboxes.

### Other Extensions

- **Footnotes** — `[^label]` references and definitions, with inline or block layout
- **Definition lists** — terms and descriptions
- **Wiki links** — `[[target]]` and `[[target|label]]` via [goldmark-wikilink](https://go.abhg.dev/goldmark/wikilink)

## Formatting Highlights

This renderer applies the same formatting rules as prettier's markdown formatter:

- ATX headings normalized with single space (`## Heading`)
- Setext headings preserved when present in source
- Emphasis uses `_underscores_`, switching to `*asterisks*` when needed for correctness
- Strong always uses `**double asterisks**`
- Unordered list markers alternate between `-` and `*` for consecutive sibling lists
- Ordered list numbers increment from the start value
- Code fence length adjusts to avoid conflicts with content
- Tables are column-aligned with padding
- `<!-- prettier-ignore -->` directives are respected

For the complete formatting specification, see [docs/FORMATTING_RULES.md](docs/FORMATTING_RULES.md).

## License

MIT
