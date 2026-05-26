package pruner

import (
	"github.com/jairo-litman/ast-analyzer/extractor"
	"github.com/jairo-litman/ast-analyzer/graph"
)

// buildImportChain returns the chain of import / re-export hops by
// which target's name enters importingFile's scope. Each hop carries
// the raw statement source. Returns ok=false when importingFile is
// target.File or when no path connects them.
func buildImportChain(p *graph.Project, importingFile string, target graph.Symbol) (ImportChain, bool) {
	return buildImportChainWithSource(p, newSourceCache(p), importingFile, target)
}

func buildImportChainWithSource(p *graph.Project, src *sourceCache, importingFile string, target graph.Symbol) (ImportChain, bool) {
	if importingFile == target.File {
		return ImportChain{}, false
	}

	for _, imp := range p.Imports {
		if imp.File != importingFile || imp.Resolved == "" {
			continue
		}
		if chain, ok := chainFromImport(p, src, imp, target); ok {
			chain.ImportingFile = importingFile
			chain.TargetSymbolID = target.ID
			chain.TargetName = target.Name
			return chain, true
		}
	}
	return ImportChain{}, false
}

// chainFromImport tries each binding of imp (named, aliased, default,
// namespace) and returns a chain when one leads to target.
func chainFromImport(p *graph.Project, src *sourceCache, imp graph.ImportEdge, target graph.Symbol) (ImportChain, bool) {
	impHop := ChainHop{
		File:      imp.File,
		Kind:      "import",
		StartByte: imp.StartByte,
		EndByte:   imp.EndByte,
		Source:    readHopSource(src, imp.File, imp.StartByte, imp.EndByte),
	}

	for _, ident := range imp.Identifiers {
		if rest, ok := followFromFile(p, src, imp.Resolved, ident.RemoteName, target, map[string]bool{}); ok {
			chain := ImportChain{LocalName: ident.LocalName, Trail: append([]ChainHop{impHop}, rest...)}
			return chain, true
		}
	}

	if imp.Namespace != "" {
		if rest, ok := followFromFile(p, src, imp.Resolved, target.Name, target, map[string]bool{}); ok {
			chain := ImportChain{LocalName: imp.Namespace, Trail: append([]ChainHop{impHop}, rest...)}
			return chain, true
		}
	}

	return ImportChain{}, false
}

// followFromFile walks re-exports from currentFile inward, looking
// for a path that lands on target.File exposing target.Name.
func followFromFile(p *graph.Project, src *sourceCache, currentFile, currentName string, target graph.Symbol, visited map[string]bool) ([]ChainHop, bool) {
	key := currentFile + "::" + currentName
	if visited[key] {
		return nil, false
	}
	visited[key] = true

	if currentFile == target.File {
		if currentName == target.Name || (currentName == "default" && target.IsDefaultExport) {
			return nil, true
		}
		return nil, false
	}

	for _, rex := range p.ReExports {
		if rex.File != currentFile || rex.Resolved == "" {
			continue
		}
		hop := ChainHop{
			File:      rex.File,
			Kind:      "re-export",
			StartByte: rex.StartByte,
			EndByte:   rex.EndByte,
			Source:    readHopSource(src, rex.File, rex.StartByte, rex.EndByte),
		}
		if next, ok := matchReExport(rex, currentName); ok {
			if rest, ok := followFromFile(p, src, rex.Resolved, next, target, visited); ok {
				return append([]ChainHop{hop}, rest...), true
			}
		}
	}
	return nil, false
}

// readHopSource returns the raw statement text or "" when no cache
// is available or the byte range is invalid. Test-callers pass nil
// for src and verify only the structural fields.
func readHopSource(src *sourceCache, file string, start, end uint) string {
	if src == nil || end <= start {
		return ""
	}
	bytes, err := src.source(file)
	if err != nil || uint(len(bytes)) < end {
		return ""
	}
	return string(bytes[start:end])
}

// matchReExport returns the name to look up in the source module
// when currentName is exposed by rex. Handles named, aliased,
// star, and `export * as ns` shapes.
func matchReExport(rex graph.ReExportEdge, currentName string) (string, bool) {
	if len(rex.Bindings) > 0 {
		for _, b := range rex.Bindings {
			if b.LocalName == currentName {
				return b.RemoteName, true
			}
		}
		return "", false
	}
	if rex.Namespace != "" {
		// `export * as ns from "..."`: only matches when the
		// consumer accessed via the namespace name.
		if currentName == rex.Namespace {
			// The whole module is exposed under rex.Namespace.
			// Following further requires a property access at the
			// call site, which isn't expressible as a single
			// name lookup — fall through to bare-star semantics
			// instead.
			return currentName, false
		}
		return "", false
	}
	// `export * from "..."`: passes every export through unchanged.
	return currentName, true
}

// _ ensures the extractor import is retained when the package
// pulls in ImportKind constants for future extension.
var _ = extractor.KindValue
