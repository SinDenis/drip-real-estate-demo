package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationDoesNotDependOnDeliveryOrInfrastructure(t *testing.T) {
	t.Parallel()

	for _, imported := range importsUnder(t, modulePath(t, "internal", "application")) {
		if strings.Contains(imported, "/internal/adapters/") || strings.Contains(imported, "/internal/platform/") {
			t.Errorf("application imports forbidden package %q", imported)
		}
	}
}

func importsUnder(t *testing.T, root string) []string {
	t.Helper()
	imports := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil || filepath.Ext(path) != ".go" {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			imports = append(imports, unquote(t, imported))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect application imports: %v", err)
	}
	return imports
}

func unquote(t *testing.T, imported *ast.ImportSpec) string {
	t.Helper()
	path, err := strconv.Unquote(imported.Path.Value)
	if err != nil {
		t.Fatalf("unquote import %q: %v", imported.Path.Value, err)
	}
	return path
}

func modulePath(t *testing.T, parts ...string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Join(append([]string{filepath.Dir(filepath.Dir(filename))}, parts...)...)
}
