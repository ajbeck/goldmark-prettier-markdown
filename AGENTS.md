# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go module for `github.com/ajbeck/goldmark-prettier-markdown`, a goldmark renderer that emits Prettier-style Markdown. Core source files live at the repository root:

- `renderer.go`: node renderers and formatting logic.
- `writer.go`: Markdown writer and prefix handling.
- `options.go`: public configuration options.
- `renderer_test.go` and `golden_test.go`: unit and golden-file tests.
- `testdata/`: Markdown input and expected output fixtures, grouped by feature.
- `docs/`: architecture, formatting rules, and contribution notes.

## Build, Test, and Development Commands

- `make fmt`: runs `go fmt ./...`.
- `make vet`: runs `go vet ./...` after formatting.
- `make test`: runs `go test ./...`; pass arguments with `make test ARGS="-run TestName"`.
- `make build`: runs `go build ./...`.
- `make all`: runs formatting, vetting, tests, and build validation.
- `make clean`: removes `.stamps/` Makefile cache files.

Use `go test ./...` directly when you do not need the Makefile's formatting and vet prerequisites.

## Coding Style & Naming Conventions

Use standard Go formatting with tabs as produced by `go fmt`. Keep exported API names clear and documented when they are part of package usage, such as `WithProseWrap` or `ProseWrapAlways`. Follow existing renderer patterns: block renderers manage prefixes and separators carefully, while inline renderers write opening and closing delimiters on enter/exit. Prefer small helpers only when they clarify repeated formatting rules.

## Testing Guidelines

Tests use Go's standard `testing` package. Add focused table-driven tests in `renderer_test.go` for behavior and golden fixtures under `testdata/<feature>/` for formatting compatibility. Fixture names follow patterns such as `basic.input.md`, `basic.golden.md`, and mode-specific outputs like `simple.always.golden.md` or `simple.never.golden.md`. Run `make test` before submitting changes.

## Commit & Pull Request Guidelines

Recent commits use concise conventional prefixes, including `docs:`, `test:`, and `chore:`; scoped forms such as `chore(ci):` are also used. Keep commit subjects imperative and specific, for example `test: add table compact mode fixtures`.

Pull requests should describe the formatting behavior changed, list relevant test coverage, and reference Prettier behavior or fixtures when applicable. For visible Markdown output changes, include before/after examples or fixture names. Update `docs/ARCHITECTURE.md` or `docs/FORMATTING_RULES.md` when changing supported node behavior or formatting rules.

## Agent-Specific Instructions

Prettier is the source of truth for formatting decisions. When behavior is unclear, compare against Prettier Markdown fixtures before changing renderer logic. Keep edits scoped, preserve existing fixtures unless intentionally updating expected output, and do not rewrite unrelated formatting rules while fixing a single node type.
