# Prettier Parity Process

Prettier is the source of truth for Markdown formatting behavior. This renderer
should match Prettier's Markdown output for supported goldmark node types, except
for embedded language formatting inside fenced code blocks. Code block contents
are preserved because goldmark exposes them as raw text and this package is not a
JavaScript, CSS, or HTML formatter.

## Audit Command

Run the local parity audit with a current Prettier binary on `PATH`:

```bash
go run ./cmd/scripts prettier-parity
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
2. Run `go run ./cmd/scripts prettier-parity` and record the Prettier version.
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

Last audit: Prettier 3.8.3 on 2026-05-30.

Status: in sync for supported behavior. The audit checked 141 fixture variants,
found 0 actionable mismatches, and reports 1 documented exception:

- Documented exception: indented code block after an empty list item. Goldmark
  parses this as a sibling indented code block, while Prettier folds the text
  into the list item as `- code`.
Future actionable mismatches are parity gaps, not accepted permanent
exceptions. Fix them in small, focused changes and rerun the parity audit after
each fix.
