# Contributing Guide

This guide is for agents (and humans) working on goldmark-prettier-markdown. It covers the development workflow, how to research prettier's behavior, and how to add new features.

## Project Overview

This is a [goldmark](https://github.com/yuin/goldmark) renderer that outputs markdown formatted to match [prettier](https://prettier.io/)'s markdown formatter. Prettier is the single source of truth — when in doubt about how something should be formatted, check prettier's output.

**Language:** Go (1.26+)
**Dependencies:** goldmark, go.abhg.dev/goldmark/wikilink
**Test command:** `go run ./cmd/scripts test`

## Setting Up the Prettier Reference

Clone prettier into `/tmp/prettier` so you can read its source and test fixtures:

```bash
git clone --depth 1 https://github.com/prettier/prettier.git /tmp/prettier
```

You will reference this constantly when implementing or debugging formatting rules.

## Key Paths in the Prettier Repo

### Source Code

All markdown formatting logic lives under `src/language-markdown/`:

| File | What it does | When to read it |
|---|---|---|
| `print/mdast.js` | **Main dispatch** — switch on node type, renders each AST node. This is the first file to read for any node type. | Always — start here for any formatting question |
| `print/children.js` | Block spacing logic — when to insert blank lines between sibling blocks | Block separator / spacing bugs |
| `print/whitespace.js` | `isBreakable()`, `lineBreakCanBeConvertedToSpace()`, `SINGLE_LINE_NODE_TYPES` — controls where line breaks can be inserted or converted to spaces | proseWrap behavior, CJK handling |
| `print/sentence.js` | `printSentence()` using `fill()` — how prose content is wrapped | proseWrap "always" mode |
| `print/word.js` | Emphasis marker selection, delimiter escaping, word-level formatting | Emphasis `_` vs `*` decisions, escaping `*`/`_` in content |
| `print/heading.js` | Heading rendering and ATX normalization | Heading formatting |
| `print/list.js` | List marker alternation, ordered list numbering, alignment | List rendering |
| `print/table.js` | Table column width measurement, alignment, compact mode | GFM table formatting |
| `print/preprocess.js` | AST preprocessing — splits text into sentences/words, detects wiki link boundaries | Understanding how prettier's AST differs from what we receive from goldmark |
| `print/ignored.js` | `<!-- prettier-ignore -->` handling | Ignore directive behavior |
| `constants.evaluate.js` | Default markers (`_` for emphasis, `**` for strong, `-` for unordered lists, `.` for ordered) | Default formatting choices |
| `options.js` | Option definitions (proseWrap, singleQuote, tabWidth, proseWrap) | Adding new options |
| `parse/unified-plugins/wiki-link.js` | Wiki link parser — regex `/^\[\[(?<linkContents>.+?)\]\]/s` | Wiki link syntax |

### Document IR (for understanding fill-wrap)

Prettier doesn't write text directly — it builds a document IR that a separate printer resolves. Understanding this is important for proseWrap "always":

| File | What it does |
|---|---|
| `document/builders/fill.js` | `fill()` document type — alternates content and whitespace, greedy line filling |
| `document/printer/printer.js` | The printer — look for `DOC_TYPE_FILL` handling to understand the greedy algorithm |
| `document/builders/index.js` | All document IR builders (hardline, softline, line, group, indent, align, etc.) |

Our `fillWrap()` function in `renderer.go` approximates prettier's fill algorithm. We use sentinel bytes (`\x00`) to mark breakable spaces, then split and greedily fill lines.

### Test Fixtures and Snapshots

Prettier's test fixtures are the ground truth for expected output:

```
tests/format/markdown/
├── auto-link/          # <url> autolinks
├── blockquote/         # > blockquotes
├── break/              # hard/soft line breaks
├── code/               # fenced and indented code blocks
├── definition/         # link reference definitions
├── delete/             # ~~strikethrough~~
├── emphasis/           # *emphasis* and _emphasis_
├── footnoteDefinition/ # [^ref]: content
├── heading/            # # ATX and setext headings
├── html/               # raw HTML blocks and inline
├── ignore/             # <!-- prettier-ignore -->
├── image/              # ![alt](url)
├── inlineCode/         # `code`
├── link/               # [text](url)
├── list/               # ordered and unordered lists
├── paragraph/          # paragraph wrapping
├── splitCjkText/       # CJK character handling
├── strong/             # **strong**
├── table/              # GFM tables
├── thematicBreak/      # --- and ***
├── wiki-link/          # [[wiki links]]
├── word/               # emphasis delimiter escaping
└── ...
```

Each directory contains `.md` input files and a `__snapshots__/format.test.js.snap` file with the expected output for each `proseWrap` mode. **Always check the snapshot file** to understand what prettier produces for a given input.

Example workflow — checking how prettier formats a footnote:
```bash
cat /tmp/prettier/tests/format/markdown/footnoteDefinition/simple.md
cat /tmp/prettier/tests/format/markdown/footnoteDefinition/__snapshots__/format.test.js.snap
```

## Project Structure

```
goldmark-prettier-markdown/
├── docs/
│   ├── ARCHITECTURE.md        # Design decisions, implementation status
│   ├── FORMATTING_RULES.md    # Complete formatting rules reference
│   └── CONTRIBUTING.md        # This file
├── renderer.go                # All render functions (organized by section)
├── writer.go                  # markdownWriter with prefix stack
├── options.go                 # Config, Option types
├── renderer_test.go           # All tests
├── go.mod
└── go.sum
```

### renderer.go Layout

All render functions are in a single file, organized by section with comment headers:

1. **Type definitions** — `Renderer`, `renderContext`, `listContext`, etc.
2. **Block node renderers** — `renderDocument`, `renderParagraph`, `renderHeading`, etc.
3. **Inline node renderers** — `renderText`, `renderEmphasis`, `renderCodeSpan`, etc.
4. **GFM extension renderers** — `renderTable`, `renderStrikethrough`, `renderTaskCheckBox`
5. **Footnote extension renderers** — `renderFootnoteList`, `renderFootnote`, etc.
6. **Definition list extension renderers** — `renderDefinitionList`, etc.
7. **Wiki link extension renderer** — `renderWikiLink`
8. **Fill-wrap infrastructure** — `beginFillWrap`, `endFillWrap`, `fillWrap`, `markBreakableSpaces`
9. **Helpers** — `writeBlockSeparator`, `handleIgnoredNode`, `writeURL`, list utilities, etc.

## How to Add a New Extension Node

1. **Find or add the goldmark extension parser.** Check if goldmark has a built-in extension (like `extension.NewFootnoteBlockParser()`) or if you need a third-party package (like `go.abhg.dev/goldmark/wikilink`).

2. **Study the AST.** Write a small debug script to parse sample input and dump the AST:
   ```go
   doc := md.Parser().Parse(text.NewReader([]byte(input)))
   doc.Dump([]byte(input), 0)
   ```
   Key things to understand: node types, child structure, what fields are available, and what information comes from source vs. the AST.

3. **Read prettier's rendering.** Open `print/mdast.js` and find the `case` for the corresponding mdast node type. Understand the formatting rules, then check the test snapshots.

4. **Register the node kind(s)** in `RegisterFuncs` at the top of `renderer.go`.

5. **Implement render functions.** Follow the pattern of existing renderers:
   - Block nodes: handle `entering` (write prefix, push prefixes) and `!entering` (pop prefixes, flush)
   - Inline nodes: handle `entering` (write opening delimiter) and `!entering` (write closing delimiter)
   - Container nodes that need manual child handling: use `WalkSkipChildren` and process children manually (like tables)

6. **Add a test helper** in `renderer_test.go` that creates a goldmark instance with the extension's parser (but NOT its HTML renderer — we provide our own renderer). Example:
   ```go
   func newTestMarkdownFootnote(opts ...prettier.Option) goldmark.Markdown {
       r := prettier.NewRenderer(opts...)
       md := goldmark.New(
           goldmark.WithRenderer(renderer.NewRenderer(
               renderer.WithNodeRenderers(util.Prioritized(r, 1000)),
           )),
       )
       md.Parser().AddOptions(
           parser.WithBlockParsers(util.Prioritized(extension.NewFootnoteBlockParser(), 999)),
           parser.WithInlineParsers(util.Prioritized(extension.NewFootnoteParser(), 101)),
           parser.WithASTTransformers(util.Prioritized(extension.NewFootnoteASTTransformer(), 999)),
       )
       return md
   }
   ```

7. **Write tests.** Every feature needs:
   - Basic rendering tests (table-driven)
   - Idempotency tests (render → re-render produces same output)
   - Edge case tests derived from prettier's snapshot fixtures

8. **Update `writeBlockSeparator`** if your new block node needs blank line separation rules. Check the switch on `parent.Kind()` at the bottom of that function.

9. **Update `docs/ARCHITECTURE.md`** to reflect the new node types.

## How to Debug Formatting Differences

When our output doesn't match prettier's:

1. **Check the snapshot.** Find the exact test case in prettier's `__snapshots__/format.test.js.snap` for the relevant proseWrap mode.

2. **Dump the goldmark AST.** Parse the input and dump it to understand what goldmark gives us. Node types, child counts, source segments, and flags like `HasBlankPreviousLines` or `IsTight` are critical.

3. **Read the prettier source for that node type.** Start at `print/mdast.js` and trace through any helper functions it calls.

4. **Check if it's an AST difference.** Prettier uses mdast, we use goldmark. Some differences:
   - Goldmark's `ast.Heading` doesn't distinguish ATX from setext; we normalize both to ATX
   - Goldmark's `FootnoteLink` has `Index` but not the ref label — we build a map from `FootnoteList`
   - Goldmark's AST transformer adds `FootnoteBacklink` nodes we need to skip
   - Goldmark's emphasis doesn't store the original marker character — we infer from source

5. **Check if it's a document IR difference.** Prettier's `group()` + `softline` means "try inline, fall back to block." We typically pre-measure width and decide before rendering.

## Key Implementation Patterns

### Prefix Stack (blockquotes, lists, footnotes, definition lists)

Block-level indentation uses `PushPrefix`/`PopPrefix` on the `markdownWriter`:

```go
// First line gets "> ", all subsequent lines also get "> "
r.rc.w.PushPrefix([]byte("> "))
// ... render children ...
r.rc.w.PopPrefix()

// First line gets "- ", continuation lines get "  "
r.rc.w.PushPrefix(prefix, 0, 0)        // line 0 only
r.rc.w.PushPrefix(contPrefix, 1)        // line 1 onward
// ... render children ...
r.rc.w.PopPrefix()
r.rc.w.PopPrefix()
```

### Fill-Wrap (proseWrap "always")

For paragraphs in "always" mode:
1. `beginFillWrap()` — redirects output to a buffer
2. Children render normally, but `renderText` marks breakable spaces with `\x00` sentinels
3. `endFillWrap()` — runs `fillWrap()` on the buffer, writes wrapped result to real writer

Spaces are non-breakable when: inside links (`singleLineDepth > 0`), between CJ characters, or before syntax-unsafe words.

### Block Separation

`writeBlockSeparator(node)` handles blank line insertion. The rules vary by parent context:
- Document/Blockquote/FootnoteList/Footnote: always blank line between children
- ListItem: blank lines only in loose lists; no blank line before nested lists
- HTML blocks: no blank line between adjacent HTML blocks
- prettier-ignore: suppresses blank line after the comment

### Idempotency

Every formatting operation must be idempotent — rendering the output a second time must produce identical results. This is tested for every feature. Common idempotency pitfalls:
- Block form decisions that depend on source line positions (these change after first render)
- Emphasis marker selection that depends on source inspection
- Footnote inline/block form decisions

## Running Tests

```bash
# Full suite
go run ./cmd/scripts test

# Specific test
go run ./cmd/scripts test -run TestFootnoteBlock -v

# No cache
go run ./cmd/scripts test ./... -count=1

# Full validation
go run ./cmd/scripts ci

# Compare fixtures with Prettier
go run ./cmd/scripts prettier-parity
```

See [PRETTIER_PARITY.md](PRETTIER_PARITY.md) for the parity process, current
known gaps, and the exact Prettier command used for comparison.

## Commit Convention

Use conventional commits with a scope:

```
feat(footnote): add footnote extension support
feat(deflist): add definition list extension support
fix: treat PrintWidth <= 0 as unlimited for compact table selection
docs: unwrap prose lines in documentation
```

## Release Process

This repository uses tag-first Go module releases. To publish a release, run the
full validation suite and push a semver tag:

```bash
go run ./cmd/scripts ci
git tag v0.1.0
git push origin v0.1.0
```

The release workflow runs on pushed `v*.*.*` tags and creates the GitHub Release
after validation passes. Do not retag an existing version; publish a new patch
version instead.
