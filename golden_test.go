package prettier_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark"

	prettier "github.com/ajbeck/goldmark-prettier-markdown"
)

var update = flag.Bool("update", false, "update golden files")

// optionVariant maps a golden file suffix to the renderer options that produce
// it.  The empty string is the default variant (proseWrap=preserve).
type optionVariant struct {
	suffix string
	opts   []prettier.Option
}

var defaultVariants = []optionVariant{
	{suffix: ""},
}

var proseWrapVariants = []optionVariant{
	{suffix: ""},
	{suffix: ".always", opts: []prettier.Option{prettier.WithProseWrap(prettier.ProseWrapAlways)}},
	{suffix: ".never", opts: []prettier.Option{prettier.WithProseWrap(prettier.ProseWrapNever)}},
}

// categoryConfig holds per-category test configuration: which parser setup to
// use and which option variants to test.
type categoryConfig struct {
	newMarkdown func(...prettier.Option) goldmark.Markdown
	variants    []optionVariant
}

var categories = map[string]categoryConfig{
	"heading":        {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"emphasis":       {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"list":           {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"table":          {newMarkdown: newTestMarkdownGFM, variants: defaultVariants},
	"blockquote":     {newMarkdown: newTestMarkdown, variants: proseWrapVariants},
	"code":           {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"inline":         {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"html":           {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"paragraph":      {newMarkdown: newTestMarkdown, variants: proseWrapVariants},
	"thematic-break": {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"ignore":         {newMarkdown: newTestMarkdown, variants: defaultVariants},
	"strikethrough":  {newMarkdown: newTestMarkdownGFM, variants: defaultVariants},
	"footnote":       {newMarkdown: newTestMarkdownFootnote, variants: proseWrapVariants},
	"deflist":        {newMarkdown: newTestMarkdownDefList, variants: defaultVariants},
	"wikilink":       {newMarkdown: newTestMarkdownWikiLink, variants: defaultVariants},
}

func TestGolden(t *testing.T) {
	dirs, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatalf("glob testdata: %v", err)
	}

	for _, dir := range dirs {
		category := filepath.Base(dir)
		cfg, ok := categories[category]
		if !ok {
			t.Errorf("testdata/%s: no category config registered", category)
			continue
		}

		inputs, err := filepath.Glob(filepath.Join(dir, "*.input.md"))
		if err != nil {
			t.Fatalf("glob inputs in %s: %v", dir, err)
		}
		if len(inputs) == 0 {
			t.Errorf("testdata/%s: no input files found", category)
			continue
		}

		t.Run(category, func(t *testing.T) {
			for _, inputPath := range inputs {
				base := filepath.Base(inputPath)
				name := strings.TrimSuffix(base, ".input.md")

				for _, v := range cfg.variants {
					v := v
					testName := name
					if v.suffix != "" {
						testName = name + v.suffix
					}

					t.Run(testName, func(t *testing.T) {
						input := mustReadFile(t, inputPath)
						md := cfg.newMarkdown(v.opts...)
						got := render(t, md, string(input))

						goldenName := name + v.suffix + ".golden.md"
						goldenPath := filepath.Join(dir, goldenName)

						if *update {
							if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
								t.Fatalf("write golden %s: %v", goldenPath, err)
							}
							return
						}

						if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
							// Golden file doesn't exist for this variant — skip.
							t.Skipf("no golden file %s (run with -update to create)", goldenName)
							return
						}

						want := string(mustReadFile(t, goldenPath))
						if got != want {
							t.Errorf("output mismatch for %s\n--- want ---\n%s--- got ---\n%s", goldenName, want, got)
						}

						// Idempotency: re-render and verify identical output.
						second := render(t, md, got)
						if second != got {
							t.Errorf("not idempotent for %s\n--- first ---\n%s--- second ---\n%s", goldenName, got, second)
						}
					})
				}
			}
		})
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
