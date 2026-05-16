package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildProject_classDecoratorsCovered pins that a class symbol's
// byte range covers any preceding `@Decorator(...)` annotations.
func TestBuildProject_classDecoratorsCovered(t *testing.T) {
	p, err := BuildProject(
		"testdata/decorators",
		"testdata/decorators/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)

	classSym := findSymbolByName(t, p, "Service")
	source := readSource(t, p.Root, classSym.File)
	rendered := string(source[classSym.StartByte:classSym.EndByte])
	assert.True(t, strings.HasPrefix(rendered, "@Injectable"),
		"class Service's symbol range should cover the @Injectable() decorator;\nrendered: %q", rendered)
}

// TestBuildProject_methodDecoratorsCovered pins that a method
// symbol's byte range covers any preceding `@Decorator` siblings.
func TestBuildProject_methodDecoratorsCovered(t *testing.T) {
	p, err := BuildProject(
		"testdata/decorators",
		"testdata/decorators/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)

	method := findSymbolByName(t, p, "compute")
	source := readSource(t, p.Root, method.File)
	rendered := string(source[method.StartByte:method.EndByte])
	assert.True(t, strings.HasPrefix(rendered, "@memoize"),
		"compute's symbol range should cover the @memoize decorator;\nrendered: %q", rendered)
}

func findSymbolByName(t *testing.T, p *Project, name string) Symbol {
	t.Helper()
	for _, s := range p.Symbols {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("symbol %q not found", name)
	return Symbol{}
}

func readSource(t *testing.T, root, relPath string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, relPath))
	require.NoError(t, err)
	return src
}
