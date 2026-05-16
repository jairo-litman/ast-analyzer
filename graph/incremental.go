package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// HashSourceFiles walks rootDir using BuildProject's filter rules and
// returns a project-relative path → sha256 hex map.
func HashSourceFiles(rootDir string) (map[string]string, error) {
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("absolute root: %w", err)
	}
	files, err := listSourceFiles(rootAbs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for _, rel := range files {
		h, err := hashFile(filepath.Join(rootAbs, rel))
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", rel, err)
		}
		out[rel] = h
	}
	return out, nil
}

// listSourceFiles returns the project-relative paths BuildProject
// would visit, in lexical order.
func listSourceFiles(rootAbs string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != rootAbs && IsSkippedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !IsIncludedFile(path) {
			return nil
		}
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func hashFile(absPath string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RemoveFile drops every Symbol, CallSite, ImportEdge, ReExportEdge,
// FileResult, and parse tree attributed to relPath. ResolvedTo
// entries in other files that pointed into relPath aren't patched
// here; rerun ResolveCalls afterward to refresh them.
func RemoveFile(p *Project, relPath string) {
	if p == nil {
		return
	}
	if p.Files != nil {
		delete(p.Files, relPath)
	}
	if p.trees != nil {
		if t := p.trees[relPath]; t != nil {
			t.Close()
		}
		delete(p.trees, relPath)
	}
	p.Symbols = filterSymbolsExcept(p.Symbols, relPath)
	p.Calls = filterCallsExcept(p.Calls, relPath)
	p.Imports = filterImportsExcept(p.Imports, relPath)
	p.ReExports = filterReExportsExcept(p.ReExports, relPath)
}

func filterSymbolsExcept(in []Symbol, file string) []Symbol {
	out := in[:0]
	for _, s := range in {
		if s.File != file {
			out = append(out, s)
		}
	}
	return out
}

func filterCallsExcept(in []CallSite, file string) []CallSite {
	out := in[:0]
	for _, c := range in {
		if c.File != file {
			out = append(out, c)
		}
	}
	return out
}

func filterImportsExcept(in []ImportEdge, file string) []ImportEdge {
	out := in[:0]
	for _, e := range in {
		if e.File != file {
			out = append(out, e)
		}
	}
	return out
}

func filterReExportsExcept(in []ReExportEdge, file string) []ReExportEdge {
	out := in[:0]
	for _, e := range in {
		if e.File != file {
			out = append(out, e)
		}
	}
	return out
}

// UpdateFiles re-parses the named files (relative to p.Root) and
// replaces their data in p. Each file's prior state is dropped before
// the fresh parse, so calling UpdateFiles on a brand-new file is safe.
// Run ResolveCalls afterward to refresh cross-file references.
func UpdateFiles(p *Project, tsconfigPath string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	if p.Files == nil {
		p.Files = map[string]*FileResult{}
	}
	pair, err := newExtractorPair(tsconfigPath)
	if err != nil {
		return fmt.Errorf("extractor: %w", err)
	}
	for _, rel := range files {
		RemoveFile(p, rel)
		absPath := filepath.Join(p.Root, rel)
		if err := processFile(pair.For(rel), p, rel, absPath); err != nil {
			return fmt.Errorf("update %s: %w", rel, err)
		}
	}
	return nil
}
