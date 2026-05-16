// Package extractor is the AST-driven core of ast-analyzer: tree-sitter
// language bindings, the embedded .scm query catalog, and the tsconfig
// path resolver.
package extractor

import (
	"embed"
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:embed queries/*.scm
var queryFiles embed.FS

// Language selects which tree-sitter grammar variant an Extractor is
// bound to. Queries are compiled per-language and not interchangeable.
type Language int

const (
	LangTypeScript Language = iota
	LangTSX
)

// Extractor bundles a tree-sitter grammar, its compiled query catalog,
// and the tsconfig-aware import resolver.
type Extractor struct {
	Language *sitter.Language
	Queries  map[string]*sitter.Query
	Resolver *Resolver
}

// NewExtractor builds an Extractor bound to the plain TypeScript
// grammar.
func NewExtractor(configPath string) (*Extractor, error) {
	return NewExtractorForLanguage(configPath, LangTypeScript)
}

// NewExtractorForLanguage builds an Extractor bound to the named
// grammar variant.
func NewExtractorForLanguage(configPath string, lang Language) (*Extractor, error) {
	ext := &Extractor{}

	if err := ext.setupLanguage(lang); err != nil {
		return nil, fmt.Errorf("failed to set up language: %w", err)
	}

	if err := ext.loadEmbeddedQueries(); err != nil {
		return nil, fmt.Errorf("failed to load embedded queries: %w", err)
	}

	if err := ext.setupResolver(configPath); err != nil {
		return nil, fmt.Errorf("failed to initialize resolver: %w", err)
	}

	return ext, nil
}

func (e *Extractor) setupLanguage(lang Language) error {
	if e.Language != nil {
		return nil
	}

	switch lang {
	case LangTypeScript:
		e.Language = sitter.NewLanguage(sitter_typescript.LanguageTypescript())
	case LangTSX:
		e.Language = sitter.NewLanguage(sitter_typescript.LanguageTSX())
	default:
		return fmt.Errorf("unknown language variant %d", lang)
	}
	if e.Language == nil {
		return fmt.Errorf("failed to load tree-sitter language %d", lang)
	}

	return nil
}

func (e *Extractor) loadEmbeddedQueries() error {
	entries, err := queryFiles.ReadDir("queries")
	if err != nil {
		return fmt.Errorf("failed to read embedded queries directory: %w", err)
	}

	e.Queries = make(map[string]*sitter.Query)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".scm") {
			continue
		}

		path := "queries/" + entry.Name()
		queryBytes, err := queryFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded query %s: %w", entry.Name(), err)
		}

		query, qerr := sitter.NewQuery(e.Language, string(queryBytes))
		if qerr != nil {
			return fmt.Errorf("failed to compile query %s: %w", entry.Name(), qerr)
		}

		name := strings.TrimSuffix(entry.Name(), ".scm")
		e.Queries[name] = query
	}

	return nil
}

func (e *Extractor) setupResolver(configPath string) error {
	resolver, err := NewResolverFromConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("failed to initialize resolver: %w", err)
	}
	e.Resolver = resolver
	return nil
}
