//go:build mage

// This magefile determines how to build and test the project.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/magefile/mage/target"
	"github.com/rkennedy/magehelper"
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

// Tidy cleans the go.mod file.
func Tidy(context.Context) error {
	return sh.RunV(mg.GoCmd(), "mod", "tidy", "-go", "1.20")
}

// Imports formats the code and updates the import statements.
func Imports(ctx context.Context) error {
	mg.CtxDeps(ctx,
		magehelper.ToolDep(goimportsBin(), "golang.org/x/tools/cmd/goimports"),
		Tidy,
	)
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

func getDependencies(
	baseMod string,
	files func(pkg magehelper.Package) []string,
	imports func(pkg magehelper.Package) []string,
) (result []string) {
	processedPackages := mapset.NewThreadUnsafeSetWithSize[string](len(magehelper.Packages))
	worklist := []string{baseMod}

	for len(worklist) > 0 {
		current := worklist[0]
		worklist = worklist[1:]
		if processedPackages.Add(current) {
			if pkg, ok := magehelper.Packages[current]; ok {
				result = append(result, expandFiles(pkg, files)...)
				worklist = append(worklist, imports(pkg)...)
			}
		}
	}
	return result
}

func expandFiles(
	pkg magehelper.Package,
	files func(pkg magehelper.Package) []string,
) []string {
	var result []string
	for _, gofile := range files(pkg) {
		result = append(result, filepath.Join(pkg.Dir, gofile))
	}
	return result
}

// Lint performs static analysis on all the code in the project.
func Lint(ctx context.Context) error {
	mg.CtxDeps(ctx,
		Generate,
		magehelper.ToolDep(reviveBin(), "github.com/mgechev/revive"),
		magehelper.LoadDependencies,
	)
	pkg, err := getBasePackage()
	if err != nil {
		return err
	}
	args := append([]string{
		"-formatter", "unix",
		"-config", "revive.toml",
		"-set_exit_status",
		"./...",
	}, magehelper.Packages[pkg].IndirectGoFiles()...)
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
	mg.CtxDeps(ctx, magehelper.LoadDependencies)
	tests := []any{}
	for _, info := range magehelper.Packages {
		tests = append(tests, mg.F(RunTest, info.ImportPath))
	}
	mg.CtxDeps(ctx, tests...)
	return nil
}

// BuildTest builds the specified package's test.
func BuildTest(ctx context.Context, pkg string) error {
	mg.CtxDeps(ctx, magehelper.LoadDependencies)
	deps := getDependencies(pkg, (magehelper.Package).TestFiles, (magehelper.Package).TestImportPackages)
	if len(deps) == 0 {
		return nil
	}

	info := magehelper.Packages[pkg]
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
	mg.CtxDeps(ctx, magehelper.LoadDependencies)
	tests := []any{}
	for _, mod := range magehelper.Packages {
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
