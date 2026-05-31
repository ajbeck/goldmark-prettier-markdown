# Architecture

## Goal: Mirror Prettier's Markdown Formatter

This project is a [goldmark](https://github.com/yuin/goldmark) renderer that produces markdown output matching [prettier](https://prettier.io/)'s markdown formatter as closely as possible. Prettier is the authoritative source of truth for all formatting decisions.

The approach is:

1. **Read prettier's source** to understand the exact formatting rules for each AST node type
2. **Translate those rules into Go** using goldmark's `renderer.NodeRenderer` interface
3. **Validate against prettier's test snapshots** to confirm our output matches

Prettier's markdown formatter lives at `src/language-markdown/` in the prettier repo. It operates on an [mdast](https://github.com/syntax-tree/mdast) AST (produced by [remark](https://github.com/remarkjs/remark)), while we operate on goldmark's AST. The ASTs differ in structure, so our implementations can't be 1:1 translations — we implement the same *formatting behavior* using goldmark's node types and walker pattern.

Key differences from prettier's architecture:

- **Prettier** uses a document IR (indent, group, fill, hardline, softline, etc.) that a separate printer resolves to text. We write directly to a line-buffered writer.
- **Prettier** has a `fill()` document type for prose wrapping. We approximate this with a sentinel-based greedy fill-wrap algorithm.
- **Prettier** uses `group()` + `softline` for conditional line breaks (e.g., footnote inline/block form). We pre-measure content width and decide before rendering.
- **Prettier** processes an mdast tree. Goldmark uses its own AST with different node types, segment-based source references, and a walker that calls `entering`/`exiting` callbacks.

---

## Design Decisions

### Interface Choice: NodeRenderer (not Renderer)

We implement `renderer.NodeRenderer` (like goldmark-adf) rather than directly implementing `renderer.Renderer` (like goldmark-markdown).

**Rationale:**
- `NodeRenderer` is the intended extension point — goldmark's built-in `renderer.Renderer` already handles AST walking and dispatching
- Registering via `NodeRendererFuncRegisterer` lets us plug into goldmark's standard composition model
- GFM extension nodes (tables, strikethrough, task checkboxes) register the same way as core nodes — no special handling
- Users can compose our renderer with other `NodeRenderer` implementations via priority ordering

**Trade-off:** goldmark-markdown's direct `Renderer` approach gave it more control over the walk and custom function signatures. We give that up in exchange for better composability. The standard `NodeRendererFunc` signature `(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error)` provides everything we need.

### Writer Design

We use a custom `markdownWriter` (inspired by goldmark-markdown) that wraps `util.BufWriter` and provides:

1. **Line-buffered output** — content buffered until newline, enabling trailing whitespace trimming
2. **Prefix stack** — for blockquote `> ` and list item indentation prefixes, with line-range scoping (prefix applies to line N through M)
3. **Trailing whitespace trimming** — every line is right-trimmed before output
4. **Prefix width measurement** — `PrefixWidth()` returns the total width of active prefixes on the current line, used by the fill-wrap algorithm to calculate available width

The writer is stored in render context, not on the renderer struct, to keep the renderer stateless across calls.

### Render Context

State that changes during rendering is carried in a `renderContext`:

```
renderContext
├── writer           *markdownWriter   // output with prefix support
├── source           []byte            // original markdown source
├── config           *Config           // options (proseWrap, printWidth, etc.)
├── listStack        []listContext     // nested list state (marker, counter)
├── ignoreRanges     []ignoreRange     // prettier-ignore-start/end pairs
├── ignoredNodes     map[ast.Node]     // nodes to skip during rendering
├── fillWrapBuf      *bytes.Buffer     // captures content for fill-wrapping
├── fillWrapWriter   *markdownWriter   // saved writer during fill-wrap
├── singleLineDepth  int              // nesting in non-breakable contexts
└── footnoteRefs     map[int][]byte   // footnote index → ref label
```

### GFM Extension Support

GFM node types are registered in `RegisterFuncs` alongside core nodes:

- `extast.KindTable` → `renderTable`
- `extast.KindTableHeader` → `renderTableHeader`
- `extast.KindTableRow` → `renderTableRow`
- `extast.KindTableCell` → `renderTableCell`
- `extast.KindStrikethrough` → `renderStrikethrough`
- `extast.KindTaskCheckBox` → `renderTaskCheckBox`

Table rendering requires a two-pass approach: first collect all cell content and measure widths, then format with column padding. This means the table renderer must skip the default walk for its children and handle them manually.

### Heading Formatting

Goldmark parses ATX and setext headings as `ast.Heading`. The renderer always
prints ATX headings to match Prettier, so setext underline source text is not
preserved.

### Emphasis Marker Selection

Prettier's emphasis marker logic requires knowledge of surrounding context (adjacent words, ancestor emphasis nodes). Our implementation:

1. Default to `_` for emphasis
2. Walk child nodes to check for autolinks
3. Check if the emphasis node has adjacent word siblings without punctuation boundaries
4. Track emphasis nesting depth in renderContext
5. For strong: always `**`

**Prettier source:** `print/word.js` (marker selection), `constants.evaluate.js` (default markers)

### List Marker Alternation

Prettier alternates unordered list markers between **consecutive source marker
runs**, NOT by nesting depth. Nested lists inherit their parent's marker style:

```
- top level
  - nested (same marker — not alternated)
    - doubly nested (same marker)

- list 1 (even sibling index = dash)

* list 2 (odd source marker run = asterisk)
```

For unordered lists, we track the source marker run because goldmark may expose
blank-separated same-marker lists as sibling nodes where Prettier's mdast keeps
them in one list. Even unordered runs use `-`; odd unordered runs use `*`. For
ordered lists, we track same-type sibling index for `.`/`)` delimiter
alternation.

**Prettier source:** `print/list.js`

### ProseWrap Modes

We support all three of prettier's `proseWrap` options:

**`preserve` (default):** Soft line breaks from the source are preserved as-is. No rewrapping.

**`never`:** Soft line breaks within paragraphs become spaces (CJK-aware — CJ-to-CJ joins produce no space). Tables use compact mode (no column padding) when aligned width exceeds printWidth.

**`always`:** Paragraphs are fill-wrapped to fit within `printWidth`. Implementation uses a sentinel-based approach:
1. During inline rendering, breakable spaces are replaced with `\x00` sentinel bytes
2. Non-breakable spaces (CJ-adjacent, inside links, before syntax-unsafe words) keep normal spaces
3. After the paragraph is fully rendered to a buffer, `fillWrap()` splits on sentinels and greedily fills lines

Syntax safety: spaces before words that would create block-level syntax at line start (blockquotes `>`, list markers `*+-`, headings `#`, ordered lists `1.`) are non-breakable. Matches prettier's regex `/^>|^(?:[*+-]|#{1,6}|\d+[).])$/`.

**Prettier source:** `print/whitespace.js` (isBreakable, lineBreakCanBeConvertedToSpace), `print/sentence.js` (fill wrapping), `document/printer/printer.js` (DOC_TYPE_FILL algorithm)

### CJK Character Classification

Prettier classifies characters into four kinds for whitespace handling: `KIND_NON_CJK`, `KIND_CJ_LETTER`, `KIND_K_LETTER`, `KIND_CJK_PUNCTUATION`.

Go implements this using `unicode` package property tables. CJK characters count as double-width for line width calculations in fill-wrap.

**Prettier source:** `print/whitespace.js`

### CommonMark Flanking Delimiter Detection

Prettier escapes internal `*` and `_` in emphasis/strong content when they could open or close emphasis per CommonMark rules. Our `canOpenOrClose` function implements the full flanking delimiter run algorithm from CommonMark 0.31.2.

**Prettier source:** `print/word.js`

### Footnote Inline vs Block Form

Prettier decides between inline (`[^ref]: content`) and block (`[^ref]:\n    content`) form based on:
- `shouldInlineFootnote`: true when single child is paragraph AND (never mode, OR preserve mode with single-line source paragraph)
- When not shouldInlineFootnote: uses `group([softline, first_child])` — inline if first child fits, block otherwise. We approximate this by pre-measuring the first paragraph's flat width against printWidth.

**Prettier source:** `print/mdast.js` (footnoteDefinition case)

---

## File Organization

```
goldmark-prettier-markdown/
├── docs/
│   ├── FORMATTING_RULES.md    # Complete formatting rules reference
│   ├── ARCHITECTURE.md        # This file
│   └── CONTRIBUTING.md        # Contributing guide for agents
├── go.mod
├── renderer.go                # NodeRenderer implementation, all render functions
├── writer.go                  # markdownWriter with prefix stack
├── options.go                 # Config, Option types, functional options
└── renderer_test.go           # Tests
```

All render functions live in `renderer.go` organized by section:
- Block node renderers (document, heading, paragraph, blockquote, code, HTML, list, thematic break)
- Inline node renderers (text, emphasis, code span, link, image, autolink, raw HTML)
- GFM extension renderers (table, strikethrough, task checkbox)
- Footnote extension renderers (footnote list, footnote, footnote link, backlink)
- Definition list extension renderers
- Wiki link extension renderer
- Fill-wrap infrastructure (beginFillWrap, endFillWrap, fillWrap, markBreakableSpaces)
- Helpers (block separator, ignore ranges, URL encoding, list utilities)

---

## Implementation Status

### Phase 1: Core Skeleton ✅

- [x] `renderer.go` — `NodeRenderer` implementation, `RegisterFuncs`, render context
- [x] `writer.go` — `markdownWriter` with prefix stack, line buffering, trailing trim
- [x] `options.go` — Config struct, `Option` interface, `WithProseWrap`, `WithSingleQuote`, `WithTabWidth`

### Phase 2: Block Elements ✅

- [x] Document (trailing newline)
- [x] Paragraph (block separator, blockquote paragraph handling)
- [x] Heading (ATX output, including setext input)
- [x] Blockquote (`> ` prefix, nesting, multiple paragraphs)
- [x] Fenced code block (backtick counting, full info string)
- [x] Indented code block (4-space prefix)
- [x] HTML block (passthrough with closure line)
- [x] Thematic break (`---` / `***` alternation in list context)
- [x] TextBlock (block separator only)
- [x] Block spacing (blank lines via HasBlankPreviousLines)
- [x] Idempotency (render output re-renders identically)

### Phase 3: Inline Elements ✅

- [x] Text (soft/hard line breaks)
- [x] String (verbatim)
- [x] Emphasis — full marker selection per prettier rules
- [x] Inline code (backtick counting, space padding per CommonMark rules)
- [x] Link (`[text](url "title")`, quote style option, URL angle-bracket wrapping)
- [x] Image (`![alt](url "title")`)
- [x] AutoLink (`<url>`)
- [x] Raw HTML (passthrough)
- [x] URL encoding (dangerous chars wrapped in `<>`, `<`/`>` encoded)
- [x] Punctuation detection (ASCII + Unicode Pc/Pd/Pe/Pf/Pi/Po/Ps categories)

### Phase 4: Lists ✅

- [x] List (marker alternation `-`/`*`, delimiter alternation `.`/`)`)
- [x] List item (prefix + continuation indent)
- [x] Tight vs. loose (via goldmark's HasBlankPreviousLines)
- [x] Git-diff-friendly ordered lists (detect `N. 1. 1. ...` pattern in source)
- [x] Aligned list prefix (pad to tabWidth boundaries for ordered lists)
- [x] Task checkbox (`[x]` / `[ ]`) — GFM stub
- [x] Plus marker normalization (`+` → `-`)

### Phase 5: GFM Tables ✅

- [x] Two-pass rendering (collect cell text, measure widths, format with padding)
- [x] Column width measurement (minimum 3 for alignment markers)
- [x] Alignment row generation (`:---`, `:--:`, `---:`, `----`)
- [x] Cell padding (left/center/right alignment with space distribution)
- [x] Pipe escaping in inline code within cells
- [x] Cell inline content rendering (emphasis, strong, code, links, images, strikethrough)
- [x] Table idempotency verified

### Phase 6: Block Spacing ✅

- [x] Blank line insertion between blocks (Document, Blockquote, ListItem contexts)
- [x] Tight list suppression (no blank lines between items)
- [x] Loose list detection (blank lines between items via goldmark's IsTight)
- [x] Nested list suppression (no blank line before nested list in list item)
- [x] Prettier-ignore `<!-- prettier-ignore -->` (suppresses blank line, next element)
- [x] HTML block sequence (no blank line between adjacent HTML blocks)
- [x] Prettier-ignore-start/end ranges (verbatim source output between comment pairs)
- [x] Extra blank line before indented code after list (prettier: shouldPrePrintTripleHardline)

### Phase 7: Emphasis Escaping + Edge Cases ✅

- [x] CommonMark flanking delimiter run detection (`canOpenOrClose`)
- [x] Internal `*`/`_` escaping in emphasis/strong (`escapeEmphasisDelimiters`)
- [x] Already-escaped delimiter detection (odd backslash count → skip)
- [x] Effective preceding/following char resolution (skip whitespace, cross node boundaries)
- [x] CJK character classification (`ClassifyRune`, `WordKind` enum)
- [x] URL encoding for dangerous characters (done in Phase 3)
- [x] Escaping idempotency verified

### Phase 8: ProseWrap Modes ✅

- [x] `proseWrap: "always"` — fill-wrap algorithm with sentinel-marked breakable spaces, CJK-aware break point detection, prefix-aware print width targeting, syntax safety guards for block-level markers
- [x] `proseWrap: "never"` — soft line breaks → space (CJK-aware), compact table mode when exceeding print width

### Phase 9: Extension Nodes ✅

- [x] Footnote support (`FootnoteList`, `Footnote`, `FootnoteLink`, `FootnoteBacklink`) — inline vs block form based on child count, paragraph line count, and proseWrap mode
- [x] Definition list support (`DefinitionList`, `DefinitionTerm`, `DefinitionDescription`) — tight/loose, multi-term, multi-description
- [x] Wiki link support (`go.abhg.dev/goldmark/wikilink`) — `[[target]]`, `[[target|label]]`, `[[target#fragment]]`, `![[embed]]`
