# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go module for `github.com/ajbeck/goldmark-prettier-markdown/v2`, a Goldmark v2 renderer that emits Prettier-style Markdown. Core source files live at the repository root:

- `renderer.go`: node renderers and formatting logic.
- `writer.go`: Markdown writer and prefix handling.
- `options.go`: public configuration options.
- `renderer_test.go` and `golden_test.go`: unit and golden-file tests.
- `testdata/`: Markdown input and expected output fixtures, grouped by feature.
- `docs/`: architecture, formatting rules, and contribution notes.
- `cmd/scripts/`: Go-native project automation for local and CI workflows.

## Build, Test, and Development Commands

- `go run ./cmd/scripts fmt`: runs `go fmt ./...`.
- `go run ./cmd/scripts vet`: runs `go vet ./...`.
- `go run ./cmd/scripts test`: runs `go test ./...`; pass arguments after the target or with `ARGS`.
- `go run ./cmd/scripts build`: runs `go build ./...`.
- `go run ./cmd/scripts ci`: runs formatting, vetting, tests, and build validation.
- `go run ./cmd/scripts clean`: removes leftover `.stamps/` cache files from the previous workflow.
- `go run ./cmd/scripts prettier-parity`: compares golden fixtures with current Prettier Markdown output.

Run `go run ./cmd/scripts help` to list targets and examples.

## Coding Style & Naming Conventions

Use standard Go formatting with tabs as produced by `go fmt`. Keep exported API names clear and documented when they are part of package usage, such as `WithProseWrap` or `ProseWrapAlways`. Follow existing renderer patterns: block renderers manage prefixes and separators carefully, while inline renderers write opening and closing delimiters on enter/exit. Prefer small helpers only when they clarify repeated formatting rules.

## Testing Guidelines

Tests use Go's standard `testing` package. Add focused table-driven tests in `renderer_test.go` for behavior and golden fixtures under `testdata/<feature>/` for formatting compatibility. Fixture names follow patterns such as `basic.input.md`, `basic.golden.md`, and mode-specific outputs like `simple.always.golden.md` or `simple.never.golden.md`. Run `go run ./cmd/scripts test` before submitting changes.

## Commit & Pull Request Guidelines

Recent commits use concise conventional prefixes, including `docs:`, `test:`, and `chore:`; scoped forms such as `chore(ci):` are also used. Keep commit subjects imperative and specific, for example `test: add table compact mode fixtures`.

Pull requests should describe the formatting behavior changed, list relevant test coverage, and reference Prettier behavior or fixtures when applicable. For visible Markdown output changes, include before/after examples or fixture names. Update `docs/ARCHITECTURE.md` or `docs/FORMATTING_RULES.md` when changing supported node behavior or formatting rules.

Releases are tag-first. Run `go run ./cmd/scripts ci` and `go run ./cmd/scripts prettier-parity`, then push an immutable semver tag such as `v2.0.0`; the release workflow validates the tagged commit and creates the GitHub Release. Use prerelease tags such as `v2.0.0-rc.1` when needed, and do not edit or recreate an existing release tag.

## Agent-Specific Instructions

Prettier is the source of truth for formatting decisions. When behavior is unclear, compare against Prettier Markdown fixtures before changing renderer logic. Use `docs/PRETTIER_PARITY.md` and `go run ./cmd/scripts prettier-parity` when changing formatting behavior. Keep edits scoped, preserve existing fixtures unless intentionally updating expected output, and do not rewrite unrelated formatting rules while fixing a single node type.
