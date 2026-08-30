# goldmark-prettier-markdown

A [goldmark](https://github.com/yuin/goldmark) renderer that outputs
[prettier](https://prettier.io/)-formatted markdown.

Instead of rendering HTML, this renderer takes a goldmark AST and writes it
back as clean, consistently formatted markdown — matching prettier's opinionated
formatting rules.

## But Why?!?

We serialise markdown to and from other formats — primarily
[Atlassian Document Format](https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/)
(ADF) — for publishing content to Confluence. One key requirement is detecting
whether a page actually needs to be updated. The problem: converting ADF back to
markdown produces inconsistently formatted output, so a naive diff against the
original source will almost always show changes, even when the content is
identical.

This renderer solves that by providing a canonical formatting pass. Run your
markdown through it before _and_ after serialisation, and you get a stable
representation that diffs cleanly.

### Why prettier's rules?

Rather than inventing our own formatting opinions, we mirror
[prettier](https://prettier.io/)'s markdown formatter — the de facto standard
for opinionated markdown formatting. This means the output is familiar to anyone
who already uses prettier, and we can validate our behaviour against prettier's
own test suite. The formatting rules are documented in detail in
[docs/FORMATTING_RULES.md](docs/FORMATTING_RULES.md).

## Installation

```bash
go get github.com/ajbeck/goldmark-prettier-markdown/v2
```

Requires Go 1.26+.

## Usage

```go
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"

	prettier "github.com/ajbeck/goldmark-prettier-markdown/v2"
)

func main() {
	r := prettier.NewRenderer()
	p := parser.New(
		parser.WithExtensions(
			extension.GFMParser,
			extension.FootnoteParser,
			extension.DefinitionListParser,
		),
	)

	input := []byte("# Hello   World\n\nSome   **bold**  text.\n")
	var buf bytes.Buffer
	if err := r.Render(&buf, input, p.Parse(input)); err != nil {
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
Enable goldmark's parser extensions, as shown in the usage example, so these
nodes are present in the AST.

### Other Extensions

- **Footnotes** — `[^label]` references and definitions, with inline or block layout
- **Definition lists** — terms and descriptions
These features require their parser extensions, as shown in the usage example.

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

## Development

Project tasks are defined in `cmd/scripts`.

```bash
go run ./cmd/scripts ci
go run ./cmd/scripts test
go run ./cmd/scripts test -run TestProseWrapAlways
go run ./cmd/scripts prettier-parity
```

Run `go run ./cmd/scripts help` for all targets.

## Releases

Releases are tag-first. After local validation, create and push an immutable Go
module semver tag:

```bash
go run ./cmd/scripts ci
go run ./cmd/scripts prettier-parity
git tag v2.0.0
git push origin v2.0.0
```

The release workflow validates the tagged commit, then creates a GitHub Release
for that tag. See [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) for the full
release process, including prereleases.

## License

MIT
