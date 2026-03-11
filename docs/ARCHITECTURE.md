# Architecture

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

The writer is stored in render context, not on the renderer struct, to keep the renderer stateless across calls.

### Render Context

State that changes during rendering is carried in a `renderContext`:

```
renderContext
├── writer       *markdownWriter   // output with prefix support
├── source       []byte            // original markdown source
├── listStack    []listContext     // nested list state (marker, counter)
├── tableContext *tableContext     // current table rendering state
└── emphasisNestDepth int         // for emphasis marker selection
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

### Setext Heading Detection

Goldmark doesn't distinguish ATX from setext headings in the AST — both are `ast.Heading`. We need to detect setext to preserve them (matching prettier).

**Approach:** For headings at level 1-2, look at the source immediately after the last content line's segment. If the very next line (after skipping a single newline and any blockquote markers/spaces) consists entirely of `=` or `-` characters, it's setext. This handles:

- Simple setext: `Hello\n=====`
- Setext inside blockquotes: `> Hello\n> =====`
- Distinguishing from ATX followed by a list: `## Section\n\n- item` (blank line between prevents false positive)

The underline itself is NOT stored in the heading node — it was consumed by the parser. To preserve it, we read it from the source using the segment positions.

### Emphasis Marker Selection

Prettier's emphasis marker logic requires knowledge of surrounding context (adjacent words, ancestor emphasis nodes). Our implementation:

1. Default to `_` for emphasis
2. Walk child nodes to check for autolinks
3. Check if the emphasis node has adjacent word siblings without punctuation boundaries (checking Text nodes adjacent to the Emphasis node's position in the source)
4. Track emphasis nesting depth in renderContext
5. For strong: always `**`

### List Marker Alternation

Prettier alternates list markers between **consecutive sibling lists**, NOT by nesting depth. Nested lists inherit their parent's marker style:

```
- top level
  - nested (same marker — not alternated)
    - doubly nested (same marker)

- list 1 (even sibling index = dash)

* list 2 (odd sibling index = asterisk)
```

We track the "nth sibling index" — counting consecutive same-type lists among the parent's children. Even indices use `-`/`.`, odd use `*`/`)`.

### CJK Character Classification

Prettier classifies characters into four kinds for whitespace handling: `KIND_NON_CJK`, `KIND_CJ_LETTER`, `KIND_K_LETTER`, `KIND_CJK_PUNCTUATION`.

**Go implementation approach:**

Go has excellent Unicode support via `unicode` and `unicode/utf8` packages. We can implement CJK detection using Unicode property tables:

```go
// CJ letter: Han, Katakana, Hiragana, Bopomofo (NOT Hangul)
func isCJLetter(r rune) bool {
    return unicode.In(r,
        unicode.Han,
        unicode.Katakana,
        unicode.Hiragana,
        unicode.Bopomofo,
    ) && !unicode.Is(unicode.Hangul, r)
}

// Korean letter: Hangul
func isKLetter(r rune) bool {
    return unicode.Is(unicode.Hangul, r)
}
```

Prettier also matches: Other_Letter, Letter_Number, Other_Symbol, Modifier_Letter, Modifier_Symbol, Nonspacing_Mark from Unicode general categories. We should include these for completeness, filtered to CJK script extensions.

For punctuation detection (CommonMark flanking rules), Go's `unicode` package provides the `Pc`, `Pd`, `Pe`, `Pf`, `Pi`, `Po`, `Ps` categories. We combine these with the ASCII punctuation set.

**Note:** CJK classification is needed even in `proseWrap: "preserve"` mode for correct emphasis marker selection (adjacent word detection). The full whitespace conversion logic is only needed for `proseWrap: "always"` (future).

### CommonMark Flanking Delimiter Detection

Prettier escapes internal `*` and `_` in emphasis/strong content when they could open or close emphasis per CommonMark rules. We implement this in Go:

```go
func isLeftFlanking(preceding, following rune) bool {
    followedByWS := isUnicodeWhitespace(following)
    followedByPunct := isPunctuation(following)
    precededByWS := isUnicodeWhitespace(preceding)
    precededByPunct := isPunctuation(preceding)

    return !followedByWS &&
        (!followedByPunct || (precededByWS || precededByPunct))
}

func isRightFlanking(preceding, following rune) bool {
    // mirror of left-flanking
}
```

For `*`: can open/close if left-flanking OR right-flanking.
For `_`: stricter — left-flanking can open only if NOT right-flanking or preceded by punctuation.

This logic runs on every word node inside emphasis/strong containers, checking each `*` or `_` character against its surrounding characters (including across node boundaries to adjacent siblings).

---

## File Organization

```
goldmark-prettier-markdown/
├── docs/
│   ├── FORMATTING_RULES.md    # Complete formatting rules reference
│   └── ARCHITECTURE.md        # This file
├── go.mod
├── renderer.go                # NodeRenderer implementation + RegisterFuncs
├── writer.go                  # markdownWriter with prefix stack
├── options.go                 # Config, Option types, functional options
├── render_block.go            # Block node renderers (heading, paragraph, etc.)
├── render_inline.go           # Inline node renderers (text, emphasis, etc.)
├── render_table.go            # GFM table rendering (two-pass)
├── render_list.go             # List rendering with marker alternation
├── emphasis.go                # Emphasis marker selection + escaping logic
├── cjk.go                     # CJK character classification (future: prose wrap)
└── renderer_test.go           # Tests
```

---

## Implementation Phases

### Phase 1: Core Skeleton ✅

- [x] `renderer.go` — `NodeRenderer` implementation, `RegisterFuncs`, render context
- [x] `writer.go` — `markdownWriter` with prefix stack, line buffering, trailing trim
- [x] `options.go` — Config struct, `Option` interface, `WithProseWrap`, `WithSingleQuote`, `WithTabWidth`

### Phase 2: Block Elements ✅

- [x] Document (trailing newline)
- [x] Paragraph (block separator, blockquote paragraph handling)
- [x] Heading (ATX default, setext preservation with underline from source)
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
- [x] Emphasis — full marker selection per prettier rules:
  - `_` default for emphasis, `**` always for strong
  - `*` when adjacent word without punctuation boundary (e.g., `a*b*c`)
  - `*` when inside strong with adjacent words (e.g., `a***b***c`)
  - `*` when nested inside another emphasis
  - Preserve original marker for autolink children
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

### Future Work

- [ ] `proseWrap: "always"` — sentence splitting, fill-mode wrapping, CJK-aware line break conversion, print width targeting
- [ ] `proseWrap: "never"` — compact table mode, single-line prose
- [ ] Footnote support
- [ ] Definition list support
- [ ] Wiki link support
