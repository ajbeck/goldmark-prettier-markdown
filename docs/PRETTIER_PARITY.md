# Prettier Parity Process

Prettier is the source of truth for Markdown formatting behavior. This renderer
should match Prettier's Markdown output for supported goldmark node types, except
for embedded language formatting inside fenced code blocks. Code block contents
are preserved because goldmark exposes them as raw text and this package is not a
JavaScript, CSS, or HTML formatter.

## Audit Command

Install the lockfile-pinned Prettier version and run the local parity audit:

```bash
npm ci
npm run prettier-parity
```

To use a specific binary:

```bash
PRETTIER=/path/to/prettier go run ./cmd/scripts prettier-parity
```

The audit compares every `testdata/**/*.golden.md` fixture against:

```bash
prettier --parser markdown --embedded-language-formatting off --prose-wrap <mode> --print-width 80
```

The `<mode>` is derived from the golden suffix: default files use `preserve`,
`.always.golden.md` uses `always`, and `.never.golden.md` uses `never`.

## Maintenance Workflow

1. Check the latest Prettier release notes and Markdown source under
   `src/language-markdown/`.
2. Update the pinned Prettier dependency deliberately, run
   `npm run prettier-parity`, and record the Prettier version.
3. For each mismatch, decide whether it is supported behavior, an intentional
   scope exclusion, or a goldmark AST limitation.
4. For supported behavior, update renderer logic and golden fixtures together.
5. Update `docs/FORMATTING_RULES.md` when a rule changes or when a documented
   rule is found to differ from Prettier.
6. Run `go run ./cmd/scripts ci` before submitting the change.

Do not silently update golden files to match current output. Golden changes
should either match Prettier output or be documented as a deliberate scope
exception.

## Current Audit

Last audit: Prettier 3.9.6 with Goldmark 2.1.5 on 2026-09-21.

The curated audit checked 153 fixture variants, found 0 new mismatches, and
reports 1 documented exception:

- Documented exception: indented code block after an empty list item. Goldmark
  parses this as a sibling indented code block, while Prettier folds the text
  into the list item as `- code`.

An exploratory sweep also compared 211 relevant fixture files from Prettier's
3.9.6 Markdown test suite using `proseWrap: preserve` and embedded-language
formatting disabled. Of those files, 177 matched exactly and 34 exposed at
least one difference. This broader result is for triage rather than a CI gate:
some upstream fixture groups use category-specific options, and a single file
can contain many independent cases.

This pass added or corrected:

- Full, collapsed, and shortcut reference links and images, including reference
  definitions and empty destinations.
- Source-style hard breaks and multiline inline-code whitespace.
- Link-title decoding, quote selection, and backslash escaping.
- Grapheme-aware table widths for CJK, emoji, and joined sequences.
- Plain GFM autolink preservation, image-alt source preservation, escaped table
  pipes, trailing empty table cells, nested-list alignment, and adjacent
  paragraph/HTML spacing.
- A lockfile-pinned Prettier 3.9.6 parity check in pull-request validation.

The remaining stable mismatches cluster around:

- Goldmark and mdast parse-tree differences for blockquote interruption,
  ambiguous setext headings, and indented code around lists.
- Complex HTML block boundaries, HTML inside tables, and ignore comments nested
  inside blockquotes.
- Source-sensitive escaping in emphasis, words, and link-reference display
  text.
- Complex list/code-block indentation, thematic-break marker alternation, and
  large mixed real-world fixtures.

Prettier's 3.10 development branch also contains unreleased Markdown changes
for links, blockquotes, setext headings, thematic breaks, and list/task/HTML
indentation. A 2026-09-21 exploratory sweep at upstream commit `51ed2cbb`
matched 178 of 222 files, with 44 mismatches. Treat those results as advance
notice; update golden expectations only after the behavior ships in a stable
Prettier release.

Future actionable mismatches are parity gaps, not accepted permanent
exceptions. Fix them in small, focused changes and rerun both the curated audit
and the relevant upstream fixtures after each fix.
