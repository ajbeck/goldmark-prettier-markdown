package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type targetFunc func([]string) error

var targets = map[string]targetFunc{
	"build":           build,
	"ci":              ci,
	"clean":           clean,
	"fmt":             format,
	"help":            help,
	"prettier-parity": prettierParity,
	"test":            test,
	"vet":             vet,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	target := "ci"
	if len(args) > 0 {
		target = args[0]
		args = args[1:]
	}

	fn, ok := targets[target]
	if !ok {
		return fmt.Errorf("unknown target %q; run \"go run ./cmd/scripts help\"", target)
	}
	return fn(args)
}

func ci(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("ci does not accept arguments")
	}
	for _, target := range []targetFunc{format, vet, test, build} {
		if err := target(nil); err != nil {
			return err
		}
	}
	return nil
}

func format(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("fmt does not accept arguments")
	}
	return runCmd("go", "fmt", "./...")
}

func vet(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("vet does not accept arguments")
	}
	return runCmd("go", "vet", "./...")
}

func build(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("build does not accept arguments")
	}
	return runCmd("go", "build", "./...")
}

func test(args []string) error {
	testArgs := []string{"test"}
	if envArgs := strings.Fields(os.Getenv("ARGS")); len(envArgs) > 0 {
		testArgs = append(testArgs, envArgs...)
	} else if len(args) > 0 {
		testArgs = append(testArgs, args...)
	} else {
		testArgs = append(testArgs, "./...")
	}
	return runCmd("go", testArgs...)
}

func clean(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("clean does not accept arguments")
	}
	return os.RemoveAll(".stamps")
}

func help(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("help does not accept arguments")
	}
	fmt.Println(`Usage: go run ./cmd/scripts [target] [args...]

Targets:
  build   verify that all packages compile
  ci      run fmt, vet, test, and build
  clean   remove leftover local workflow cache files
  fmt     format all Go packages
  help    show this help text
  prettier-parity
          compare golden fixtures with prettier markdown output
  test    run go test; extra args or ARGS are passed to go test
  vet     run go vet on all packages

Examples:
  go run ./cmd/scripts ci
  go run ./cmd/scripts test
  go run ./cmd/scripts test -run TestProseWrapAlways -v
  go run ./cmd/scripts prettier-parity
  ARGS="-run TestProseWrapAlways -v" go run ./cmd/scripts test`)
	return nil
}

type parityCase struct {
	input  string
	golden string
	mode   string
}

var parityExceptions = map[string]string{
	"testdata/code/indented-after-list.golden.md": "goldmark parses the indented code as a sibling block after an empty list item; Prettier folds it into the list item",
}

func prettierParity(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("prettier-parity does not accept arguments")
	}

	prettier := os.Getenv("PRETTIER")
	if prettier == "" {
		prettier = "prettier"
	}

	version, err := exec.Command(prettier, "--version").Output()
	if err != nil {
		return fmt.Errorf("running %s --version: %w", prettier, err)
	}
	fmt.Printf("Prettier: %s", version)

	cases, err := parityCases("testdata")
	if err != nil {
		return err
	}

	var mismatches []parityCase
	var exceptions []parityCase
	for _, c := range cases {
		got, err := prettierMarkdown(prettier, c.input, c.mode)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(c.golden)
		if err != nil {
			return fmt.Errorf("reading %s: %w", c.golden, err)
		}
		if bytes.Equal(want, got) {
			continue
		}
		if _, ok := parityExceptions[c.golden]; ok {
			exceptions = append(exceptions, c)
		} else {
			mismatches = append(mismatches, c)
		}
	}

	fmt.Printf("Checked %d fixture variants\n", len(cases))
	if len(exceptions) > 0 {
		fmt.Printf("Documented exceptions: %d\n", len(exceptions))
		for _, c := range exceptions {
			fmt.Printf("- %s: %s\n", c.golden, parityExceptions[c.golden])
		}
	}
	if len(mismatches) == 0 {
		fmt.Println("Prettier parity passed")
		return nil
	}

	fmt.Printf("Prettier parity failed: %d mismatches\n", len(mismatches))
	for _, c := range mismatches {
		fmt.Printf("- %s (mode=%s, input=%s)\n", c.golden, c.mode, c.input)
	}
	return fmt.Errorf("prettier parity failed")
}

func parityCases(root string) ([]parityCase, error) {
	var inputs []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".input.md") {
			return nil
		}
		inputs = append(inputs, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	slices.Sort(inputs)

	var cases []parityCase
	variants := []struct {
		suffix string
		mode   string
	}{
		{mode: "preserve"},
		{suffix: ".always", mode: "always"},
		{suffix: ".never", mode: "never"},
	}
	for _, input := range inputs {
		base := strings.TrimSuffix(input, ".input.md")
		for _, v := range variants {
			golden := base + v.suffix + ".golden.md"
			if _, err := os.Stat(golden); err == nil {
				cases = append(cases, parityCase{input: input, golden: golden, mode: v.mode})
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("checking %s: %w", golden, err)
			}
		}
	}
	return cases, nil
}

func prettierMarkdown(prettier, input, mode string) ([]byte, error) {
	args := []string{
		"--parser", "markdown",
		"--embedded-language-formatting", "off",
		"--prose-wrap", mode,
		"--print-width", "80",
		input,
	}
	cmd := exec.Command(prettier, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running prettier for %s (%s): %w\n%s", input, mode, err, stderr.String())
	}
	return out, nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
