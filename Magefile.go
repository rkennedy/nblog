//go:build mage

// This magefile determines how to build and test the project.
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
		_, _ = fmt.Printf(s, args...)
	}
}

// Package represents the output from go list -json
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

func loadDependencies(context.Context) error {
	dependencies, err := sh.Output(mg.GoCmd(), "list", "-json", "./...")
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(dependencies))
	for {
		var pkg Package
		switch err = dec.Decode(&pkg); err {
		case io.EOF:
			return nil
		case nil:
			packages[pkg.ImportPath] = pkg
		default:
			return err
		}
	}
}

// Tidy cleans the go.mod file.
func Tidy(context.Context) error {
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

// indirectGoFiles returns the files that aren't automatically selected as being part of the package proper. If we
// include _all_ source files here, then Revive interprets each file in isolation, which affects some of its analyses
// that apply to the package as a whole (such as the package-comments rule). Thus, we use "./..." to request Revive
// analyze all the package files, and then augment the request with the files from this function get "everything else."
func (pkg Package) indirectGoFiles() []string {
	files := append(pkg.TestGoFiles, pkg.XTestGoFiles...)
	return append(files, pkg.IgnoredGoFiles...)
}

type set[T comparable] struct {
	values map[T]any
}

func (s *set[T]) Insert(t T) bool {
	if len(s.values) != 0 {
		_, ok := s.values[t]
		if ok {
			return false
		}
	} else {
		s.values = map[T]any{}
	}
	s.values[t] = nil
	return true
}

func getDependencies(
	baseMod string,
	files func(pkg Package) []string,
	imports func(pkg Package) []string,
) (result []string) {
	var processedPackages set[string]
	worklist := []string{baseMod}

	for len(worklist) > 0 {
		current := worklist[0]
		worklist = worklist[1:]
		if processedPackages.Insert(current) {
			if pkg, ok := packages[current]; ok {
				result = append(result, expandFiles(pkg, files)...)
				worklist = append(worklist, imports(pkg)...)
			}
		}
	}
	return result
}

func expandFiles(
	pkg Package,
	files func(pkg Package) []string,
) []string {
	var result []string
	for _, gofile := range files(pkg) {
		result = append(result, filepath.Join(pkg.Dir, gofile))
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
		"./...",
	}, packages[pkg].indirectGoFiles()...)
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
		return nil
	}

	info := packages[pkg]
	exe := filepath.Join(info.Dir, info.Name+".test")

	newer, err := target.Path(exe, deps...)
	if err != nil || !newer {
		return err
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
func Check(ctx context.Context) {
	mg.CtxDeps(ctx, Test, Lint)
}

// All runs the build, test, and lint targets.
func All(ctx context.Context) {
	mg.CtxDeps(ctx, Test, Lint)
}

// Generate creates all generated code files.
func Generate(ctx context.Context) {
	mg.CtxDeps(ctx, Imports)
}

func currentFileVersion(bin string) (string, error) {
	binInfo, err := buildinfo.ReadFile(bin)
	if err != nil {
		// Either file doesn't exist or we couldn't read it. Either way, we want to install it.
		logV("%v\n", err)
		err = sh.Rm(bin)
		if err != nil {
			return "", err
		}
	}
	logV("%s version %s\n", bin, binInfo.Main.Version)
	return binInfo.Main.Version, nil
}

func configuredModuleVersion(module string) (string, error) {
	listOutput, err := sh.Output(mg.GoCmd(), "list", "-f", "{{.Module.Version}}", module)
	if err != nil {
		return "", err
	}
	logV("module version %s\n", listOutput)
	return listOutput, nil
}

func installTool(bin, module string) error {
	fileVersion, err := currentFileVersion(bin)
	if err != nil {
		return err
	}

	moduleVersion, err := configuredModuleVersion(module)
	if err != nil {
		return err
	}

	if fileVersion == moduleVersion {
		logV("Command is up to date.\n")
		return nil
	}
	return installModule(module)
}

func installModule(module string) error {
	logV("Installing %s\n", module)
	gobin, err := filepath.Abs("./bin")
	if err != nil {
		return err
	}
	return sh.RunWithV(map[string]string{"GOBIN": gobin}, mg.GoCmd(), "install", module)
}

// Goimports installs the goimports tool.
func Goimports(context.Context) error {
	module := "golang.org/x/tools/cmd/goimports"
	return installTool(goimportsBin(), module)
}

// Revive installs the revive linting tool.
func Revive(context.Context) error {
	module := "github.com/mgechev/revive"
	return installTool(reviveBin(), module)
}
