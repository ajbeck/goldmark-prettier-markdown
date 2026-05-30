package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type targetFunc func([]string) error

var targets = map[string]targetFunc{
	"build": build,
	"ci":    ci,
	"clean": clean,
	"fmt":   format,
	"help":  help,
	"test":  test,
	"vet":   vet,
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
  test    run go test; extra args or ARGS are passed to go test
  vet     run go vet on all packages

Examples:
  go run ./cmd/scripts ci
  go run ./cmd/scripts test
  go run ./cmd/scripts test -run TestProseWrapAlways -v
  ARGS="-run TestProseWrapAlways -v" go run ./cmd/scripts test`)
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
