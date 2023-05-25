//go:build mage

package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/magefile/mage/target"
	"golang.org/x/mod/modfile"
)

func goimportsBin() string {
	return path.Join("bin", "goimports")
}

func reviveBin() string {
	return path.Join("bin", "revive")
}

func logV(s string, args ...any) {
	if mg.Verbose() {
		fmt.Printf(s, args...)
	}
}

type Package struct {
	Dir        string
	ImportPath string
	Name       string
	Target     string

	GoFiles        []string
	IgnoredGoFiles []string
	TestGoFiles    []string
	XTestGoFiles   []string

	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string

	Imports      []string
	TestImports  []string
	XTestImports []string
}

var packages = map[string]Package{}

func loadDependencies(ctx context.Context) error {
	dependencies, err := sh.Output(mg.GoCmd(), "list", "-json", "./...")
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(dependencies))
	for {
		var pkg Package
		err = dec.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		packages[pkg.ImportPath] = pkg
	}

	return nil
}

// Tidy cleans the go.mod file.
func Tidy(ctx context.Context) error {
	return sh.RunV(mg.GoCmd(), "mod", "tidy", "-go", "1.20")
}

// Imports formats the code and updates the import statements.
func Imports(ctx context.Context) error {
	mg.CtxDeps(ctx, Goimports, Tidy)
	return sh.RunV(goimportsBin(), "-w", "-l", ".")
}

func getBasePackage() (string, error) {
	f, err := os.Open("go.mod")
	if err != nil {
		return "", err
	}
	defer f.Close()

	bytes, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	return modfile.ModulePath(bytes), nil
}

func (pkg Package) sourceFiles() []string {
	return append(pkg.GoFiles, pkg.EmbedFiles...)
}

func (pkg Package) sourceImports() []string {
	return pkg.Imports
}

func (pkg Package) testFiles() []string {
	return append(pkg.TestGoFiles, pkg.XTestGoFiles...)
}

func (pkg Package) testImports() []string {
	return append(pkg.TestImports, pkg.XTestImports...)
}

func (pkg Package) allGoFiles() []string {
	files := append(pkg.GoFiles, pkg.TestGoFiles...)
	files = append(files, pkg.IgnoredGoFiles...)
	return append(files, pkg.XTestGoFiles...)
}

func getDependencies(baseMod string, files func(pkg Package) []string, imports func(pkg Package) []string) []string {
	processedPackages := map[string]struct{}{}
	worklist := []string{baseMod}

	var result []string
	for len(worklist) > 0 {
		current := worklist[0]
		worklist = worklist[1:]
		if _, ok := processedPackages[current]; ok {
			continue
		}
		processedPackages[current] = struct{}{}

		if pkg, ok := packages[current]; ok {
			for _, gofile := range files(pkg) {
				result = append(result, filepath.Join(pkg.Dir, gofile))
			}
			worklist = append(worklist, imports(pkg)...)
		}
	}
	return result
}

// Lint performs static analysis on all the code in the project.
func Lint(ctx context.Context) error {
	mg.CtxDeps(ctx, Generate, Revive, loadDependencies)
	pkg, err := getBasePackage()
	if err != nil {
		return err
	}
	args := append([]string{
		"-formatter", "unix",
		"-config", "revive.toml",
		"-set_exit_status",
	}, packages[pkg].allGoFiles()...)
	return sh.RunWithV(
		map[string]string{
			"REVIVE_FORCE_COLOR": "1",
		},
		reviveBin(),
		args...,
	)
}

// Test runs unit tests.
func Test(ctx context.Context) error {
	mg.CtxDeps(ctx, loadDependencies)
	tests := []any{}
	for _, info := range packages {
		tests = append(tests, mg.F(RunTest, info.ImportPath))
	}
	mg.CtxDeps(ctx, tests...)
	return nil
}

// BuildTest builds the specified package's test.
func BuildTest(ctx context.Context, pkg string) error {
	mg.CtxDeps(ctx, loadDependencies)
	deps := getDependencies(pkg, (Package).testFiles, (Package).testImports)
	if len(deps) == 0 {
		logV("No test source files for %s.\n", pkg)
		return nil
	}

	info := packages[pkg]
	exe := filepath.Join(info.Dir, info.Name+".test")

	newer, err := target.Path(exe, deps...)
	if err != nil {
		return err
	}
	if !newer {
		logV("Target is up to date.\n")
		return nil
	}
	return sh.RunV(
		mg.GoCmd(),
		"test",
		"-c",
		"-o", exe,
		pkg)
}

// RunTest runs the specified package's tests.
func RunTest(ctx context.Context, pkg string) error {
	mg.CtxDeps(ctx, mg.F(BuildTest, pkg))

	return sh.RunV(mg.GoCmd(), "test", "-timeout", "10s", pkg)
}

// BuildTests build all the tests.
func BuildTests(ctx context.Context) error {
	mg.CtxDeps(ctx, loadDependencies)
	tests := []any{}
	for _, mod := range packages {
		tests = append(tests, mg.F(BuildTest, mod.ImportPath))
	}
	mg.CtxDeps(ctx, tests...)
	return nil
}

// Check runs the test and lint targets.
func Check(ctx context.Context) { //nolint:deadcode // Exported for Mage.
	mg.CtxDeps(ctx, Test, Lint)
}

// All runs the build, test, and lint targets.
func All(ctx context.Context) { //nolint:deadcode // Exported for Mage.
	mg.CtxDeps(ctx, Test, Lint)
}

// Generate creates all generated code files.
func Generate(ctx context.Context) {
	mg.CtxDeps(ctx, Imports)
}

func installTool(bin, module string) error {
	if binInfo, err := buildinfo.ReadFile(bin); err != nil {
		// Either file doesn't exist or we couldn't read it. Either way, we want to install it.
		logV("%v\n", err)
		err = sh.Rm(bin)
		if err != nil {
			return err
		}
	} else {
		logV("%s version %s\n", bin, binInfo.Main.Version)

		listOutput, err := sh.Output(mg.GoCmd(), "list", "-f", "{{.Module.Version}}", module)
		if err != nil {
			return err
		}
		logV("module version %s\n", listOutput)

		if binInfo.Main.Version == listOutput {
			logV("Command is up to date.\n")
			return nil
		}
	}
	logV("Installing\n")
	gobin, err := filepath.Abs("./bin")
	if err != nil {
		return err
	}
	return sh.RunWithV(map[string]string{"GOBIN": gobin}, "go", "install", module)
}

// Goimports installs the goimports tool.
func Goimports(ctx context.Context) error {
	module := "golang.org/x/tools/cmd/goimports"
	return installTool(goimportsBin(), module)
}

// Revive installs the revive linting tool.
func Revive(ctx context.Context) error {
	module := "github.com/mgechev/revive"
	return installTool(reviveBin(), module)
}
