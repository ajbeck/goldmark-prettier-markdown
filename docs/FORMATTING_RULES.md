# Prettier Markdown Formatting Rules

This document defines every formatting rule our renderer applies, derived from [prettier's markdown formatter](https://github.com/prettier/prettier/tree/main/src/language-markdown).

Each rule references the prettier source file it was extracted from.

---

## 1. Headings

**Source:** `print/heading.js`, `print/mdast.js`

### ATX Headings

- Preserve the ATX prefix style: `# H1`, `## H2`, ... `###### H6`
- Always exactly one space between `#` markers and content
- No closing `#` markers
- Preserve setext headings, normalizing their underline to `=` (level 1) or
  `-` (level 2)

```markdown
# ATX heading

Setext heading
==============
```

---

## 2. Emphasis and Strong

**Source:** `print/mdast.js`, `print/word.js`, `constants.evaluate.js`

### Emphasis (Italic)

- **Default marker:** `_` (underscore)
- **Switch to `*`** when any of these conditions hold:
  1. Content is an autolink (e.g., `*<http://example.com>*`)
  2. Adjacent word nodes lack trailing/leading punctuation — prevents `1_2_3` from being interpreted as emphasis; `1*2*3` is valid emphasis
  3. Nested inside another emphasis element
  4. Inside a strong element that itself has adjacent words without punctuation

```markdown
_normal emphasis_
*emphasis near words like 1*2*3*
*<http://example.com>*
_outer *inner nested* outer_
```

### Strong (Bold)

- Always `**double asterisks**` — never `__underscores__`

### Strikethrough (GFM)

- Always `~~double tildes~~`

### Escaping Inside Emphasis/Strong

**Source:** `print/word.js`

Internal `*` or `_` characters that could open or close emphasis per CommonMark flanking delimiter run rules must be backslash-escaped.

**CommonMark flanking rules** (spec 0.31.2):
- **Left-flanking:** not followed by whitespace AND (not followed by punctuation OR preceded by whitespace/punctuation)
- **Right-flanking:** not preceded by whitespace AND (not preceded by punctuation OR followed by whitespace/punctuation)
- `*` can open/close if either left-flanking or right-flanking
- `_` has stricter rules: left-flanking can open only if not right-flanking OR preceded by punctuation; right-flanking can close only if not left-flanking OR followed by punctuation

**Punctuation character set:**
- ASCII: `!"#$%&'()*+,-./:;<=>?@[\]^_`{|}~` plus U+3000 (ideographic space) and U+FF5E (fullwidth tilde)
- Unicode categories: Pc (Connector), Pd (Dash), Pe (Close), Pf (Final), Pi (Initial), Po (Other), Ps (Open)

---

## 3. Inline Code

**Source:** `print/mdast.js`

- Surround with minimum backticks not present as a continuous run in the content (e.g., if content contains `` ` ``, use ` `` `)
- **Pad with a space** when:
  - Content starts or ends with a backtick character
  - Content has leading AND trailing whitespace/newline AND contains non-whitespace
- Newlines in inline code collapsed to spaces (except when `proseWrap: "preserve"`)
- Pipes (`|`) escaped as `\|` inside table cells

```markdown
`simple code`
`` code with ` backtick ``
` code with spaces `
```

---

## 4. Links

**Source:** `print/mdast.js`

### Autolinks

- Preserved as `<URL>` when detected (link text equals URL, single child)
- Mailto prefix stripped if not in original source

### Regular Links

- Format: `[text](url "title")` or `[text](url 'title')`
- **URL wrapping:** Wrap URL in `<>` if it contains spaces or dangerous characters (`)`, `<`, `>`)
- Encode `<` and `>` in angle-bracket URLs

### Link Titles

- Quote style controlled by `singleQuote` option (default: double quotes)
- If title contains both `"` and `'` but not `)`, use parentheses: `(title)`
- Backslashes and chosen quote character are escaped within title

### Link References

- `[text][ref]` (full), `[text][]` (collapsed), `[text]` (shortcut)
- Label whitespace collapsed; brackets and backslashes escaped in label

### Image Links

- Format: `![alt](url "title")`
- Original alt text preserved from source when available

---

## 5. Lists

**Source:** `print/list.js`, `print/mdast.js`, `utilities.js`

### Unordered Lists

- **Marker alternation between consecutive source marker runs** (NOT by nesting):
  - Even-indexed source marker runs: `-`
  - Odd-indexed source marker runs: `*`
- Consecutive same-marker unordered lists stay in the same run; changing the
  source marker (`-`, `*`, or `+`) starts the next run
- Nested lists inherit the marker from their sibling index context (typically 0 → `-`)
- All marker types (`-`, `*`, `+`) normalize to `-` or `*`

```markdown
- item 1
- item 2
  - nested (same marker, not alternated)

- first list (index 0 = dash)

* second source marker run (index 1 = asterisk)
```

### Ordered Lists

- Numbers increment from `start` value: `1. `, `2. `, `3. `, ...
- **Delimiter alternation by nesting depth:**
  - Even-indexed: `.` (e.g., `1. `)
  - Odd-indexed: `)` (e.g., `1) `)
- **Git-diff-friendly lists:** Pattern `0. 1. 1. ...` preserved (repeated `1.` after first item for clean diffs)
- **Aligned lists:** When detected as aligned, prefix padded to `tabWidth` boundaries

### List Item Content

- Checkbox syntax: `[x] ` (checked) or `[ ] ` (unchecked)
- Content indented to align with prefix length
- Continuation lines aligned with content start
- Maximum 3 leading/trailing spaces in prefix to avoid accidental code blocks (4+ spaces triggers indented code block interpretation)

### Loose vs. Tight Lists

- **Tight lists:** No blank line between items
- **Loose lists:** Blank line between items (when `spread: true` or gap > 1 line between items in source)
- Preserved from source

---

## 6. Code Blocks

**Source:** `print/mdast.js`

### Fenced Code Blocks

- Delimiter: backticks (`` ` ``), minimum 3
- If content contains a run of backticks, use one more than the longest run
- Format:
  ```
  ```lang meta
  content
  ```
  ```
- Language identifier and metadata preserved
- Content newlines preserved as-is

### Indented Code Blocks

- 4-space indent on every line
- Preserved as indented (not converted to fenced)
- Extra blank line required before indented code block that follows a list

---

## 7. Blockquotes

**Source:** `print/mdast.js`

- Prefix: `> ` (greater-than + space)
- Continuation lines aligned with `> ` prefix
- Children rendered with `> ` alignment
- Nested blockquotes handled recursively

```markdown
> First level
>
> > Nested blockquote
```

---

## 8. Thematic Breaks (Horizontal Rules)

**Source:** `print/mdast.js`

- Default: `---` (three dashes)
- **Alternation in list context:**
  - Even-indexed sibling lists: `***`
  - Odd-indexed sibling lists: `---`
- Outside list context: always `---`

---

## 9. Hard Breaks

**Source:** `print/mdast.js`

- If original source uses trailing spaces: `  \n` (two spaces + newline)
- If original source uses backslash: `\` + newline

---

## 10. Tables (GFM)

**Source:** `print/table.js`

### Structure

- Format: `| cell1 | cell2 | cell3 |`
- Space after opening `|` and before closing `|`

### Column Alignment

- Delimiter row reflects alignment:
  - Left: `:--` (colon + dashes)
  - Center: `:-:` (colon + dashes + colon)
  - Right: `--:` (dashes + colon)
  - None: `---` (dashes only)
- Minimum column width: 3 characters (to fit alignment markers)

### Column Width Padding

- Columns padded to the maximum content width in that column
- Alignment-aware padding:
  - Left-aligned: text left, spaces right
  - Center-aligned: spaces distributed (extra space goes right)
  - Right-aligned: spaces left, text right

### Compact Mode (Future: proseWrap: "never")

- When `proseWrap: "never"` and table exceeds print width, use compact mode with minimum cell spacing

---

## 11. Block Spacing

**Source:** `print/children.js`

### Blank Line Rules

- **Single blank line** between block elements (paragraph, heading, code block, blockquote, list, thematic break, HTML block)
- **No blank line** when:
  - Consecutive sibling nodes of type `listItem` or `definition`
  - Inside tight list items (except before nested lists)
  - Previous node is a prettier-ignore directive
  - Consecutive HTML blocks without blank line between them in source
  - HTML block immediately after paragraph without blank line in source
- **Extra blank line** (double hardline) when:
  - Previous node is a loose list item
  - Current is a list inside a list item following a code block
- **Triple hardline** (extra extra blank line) when:
  - Previous node is a list AND current is an indented code block

---

## 12. HTML Blocks

**Source:** `print/mdast.js`

- HTML comments: preserved with hard line breaks
- Other HTML blocks: preserved with literal line breaks (marked as root to avoid indentation)
- Trailing HTML at document root: trimmed at end

---

## 13. Whitespace and Prose Wrap

**Source:** `print/whitespace.js`, `print/sentence.js`, `print/paragraph.js`

### Prose Wrap Modes

#### `"preserve"` (Default — Only Mode for v1)

- Original newlines in prose preserved as hardlines
- Spaces preserved as spaces
- No automatic line wrapping

#### `"always"` (Future)

- Line breaks become spaces in prose (when safe to convert)
- Text wraps to `printWidth` at space boundaries
- Fill-mode line breaking via sentence/paragraph flattening

#### `"never"` (Future)

- No automatic line breaks at spaces
- Line breaks within inline contexts converted to spaces

### CJK-Aware Whitespace Rules

**Source:** `print/whitespace.js`, `utilities.js`

These rules apply when `proseWrap: "always"` (future feature), but the character classification is needed for correctness even in "preserve" mode.

#### Character Classification

Text is split into word nodes classified by kind:
- `KIND_NON_CJK`: Latin, numbers, ASCII punctuation sequences
- `KIND_CJ_LETTER`: Chinese/Japanese characters (Han, Katakana, Hiragana, Bopomofo — NOT Korean)
- `KIND_K_LETTER`: Korean Hangul characters
- `KIND_CJK_PUNCTUATION`: CJK-specific punctuation characters

**CJK detection regex** covers:
- Script_Extensions: Han, Katakana, Hiragana, Hangul, Bopomofo
- General_Category: Other_Letter, Letter_Number, Other_Symbol, Modifier_Letter, Modifier_Symbol, Nonspacing_Mark
- Variation Selectors (optional suffix)

#### Line Break → Space Conversion Rules

When converting newlines to spaces:

| Previous | Next | Convert to space? |
|----------|------|-------------------|
| Non-CJK or Korean | Non-CJK or Korean | Yes |
| Korean | CJ | Yes |
| CJ | Korean | Yes |
| CJK punctuation | Any | No |
| Any | CJK punctuation | No |
| CJ | CJ | No |
| CJ ↔ ASCII punctuation | (either direction) | Yes |
| CJ ↔ Unicode punctuation | (either direction) | No |
| CJ ↔ Non-CJK | (no punctuation) | Check sentence context |

**Sentence context check:** If the parent sentence predominantly uses spaces between CJ and non-CJK characters (more space nodes than empty nodes), treat the newline as a space. Otherwise, use empty string (no space).

#### "Fake" Whitespace (Empty String Nodes)

Between CJK characters split from non-whitespace-separated text, empty string whitespace nodes are inserted. These are NOT converted to spaces — they represent CJK character joining without visible separation.

### Single-Line Constraints

In these contexts, all newlines must become spaces (no line breaks allowed):
- Table cells
- Links
- ATX headings

---

## 14. Prettier-Ignore Directives

**Source:** `print/mdast.js`, `utilities.js`

- `<!-- prettier-ignore -->` — ignore next element
- `<!-- prettier-ignore-start -->` / `<!-- prettier-ignore-end -->` — ignore range
- Ignored elements rendered verbatim from original source

---

## 15. Document Structure

**Source:** `print/mdast.js`

- Empty documents produce empty string
- Document ends with a single trailing newline (hardline)
- Root-level processing handles prettier-ignore ranges

---

## 16. Footnotes

**Source:** `print/mdast.js`

- Reference: `[^label]`
- Definition: `[^label]: content`
- Inline when: single paragraph child or simple blockquote child fits on one line
- Multi-line: content indented with 4-space alignment

---

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `proseWrap` | string | `"preserve"` | `"preserve"` / `"always"` / `"never"` |
| `singleQuote` | bool | `false` | Use single quotes for link/image titles |
| `tabWidth` | int | `2` | Tab width for list alignment calculations |

### Future Options (Not in v1)

- `proseWrap: "always"` — requires full prose wrap engine with CJK support
- `proseWrap: "never"` — requires compact table mode
- `printWidth` — line width target for `proseWrap: "always"` (default 80)
