package pruner

import (
	"sort"
	"strings"

	"github.com/jairo-litman/ast-analyzer/graph"
)

// collectTypesBFS walks the target's TypeRefs outward up to
// opts.TypeDepth hops, one TypeEntry per distinct referenced symbol.
// For type-kind targets, extends / implements relationships always
// surface at depth 1 (structural minimum) regardless of TypeDepth,
// since they are part of the type's own declaration. Cycles
// terminate via a visited set seeded with the target.
func collectTypesBFS(p *graph.Project, cache *sourceCache, target graph.Symbol, opts ExtractOptions) ([]TypeEntry, error) {
	structuralOnly := opts.TypeDepth <= 0
	if structuralOnly && !isTypeKind(target.Kind) {
		return nil, nil
	}

	visited := map[string]bool{target.ID: true}
	entriesByID := map[string]*TypeEntry{}
	var order []string

	frontier := []string{target.ID}
	maxDepth := opts.TypeDepth
	if structuralOnly {
		maxDepth = 1
	}

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		buckets := map[string][]graph.TypeOrigin{}
		for _, currentID := range frontier {
			sym, ok := lookupSymbol(p, currentID)
			if !ok {
				continue
			}
			for _, sref := range sym.TypeRefs {
				// At depth 1 with TypeDepth=0, only structural slots
				// contribute. Once we descend further, all slots count.
				if structuralOnly && depth == 1 && !isStructuralSlot(sref.Slot) {
					continue
				}
				sref.Ref.WalkBaseTypes(func(node *graph.TypeRef) {
					for _, targetID := range node.Targets {
						if visited[targetID] {
							continue
						}
						origin := node.Origin
						if origin == "" {
							origin = graph.OriginAnnotation
						}
						buckets[targetID] = append(buckets[targetID], origin)
					}
				})
			}
		}

		ids := truncateTypeBucketsByID(buckets, opts.MaxPerLevel)

		var next []string
		for _, id := range ids {
			sym, ok := lookupSymbol(p, id)
			if !ok {
				continue
			}
			visited[id] = true
			source, err := cache.symbolSource(sym)
			if err != nil {
				return nil, err
			}
			entry := &TypeEntry{
				Symbol:  sym,
				Source:  source,
				File:    sym.File,
				Depth:   depth,
				Origins: dedupOrigins(buckets[id]),
			}
			entriesByID[id] = entry
			order = append(order, id)
			next = append(next, id)
		}
		frontier = next
	}

	if len(order) == 0 {
		return nil, nil
	}
	out := make([]TypeEntry, 0, len(order))
	for _, id := range order {
		out = append(out, *entriesByID[id])
	}
	return out, nil
}

// isTypeKind reports whether the symbol kind names a TS type
// declaration (as opposed to a function or synthetic module).
func isTypeKind(k graph.SymbolKind) bool {
	switch k {
	case graph.SymbolClass, graph.SymbolInterface, graph.SymbolTypeAlias, graph.SymbolEnum:
		return true
	}
	return false
}

// isStructuralSlot reports whether a SymbolTypeRef.Slot describes a
// structural relationship (extends / implements / type-alias RHS)
// rather than an external dependency like a property type.
func isStructuralSlot(slot string) bool {
	switch {
	case slot == "extends":
		return true
	case strings.HasPrefix(slot, "extends:"):
		return true
	case strings.HasPrefix(slot, "implements:"):
		return true
	case slot == "value":
		return true
	}
	return false
}

// truncateTypeBucketsByID returns bucket keys sorted ascending,
// capped at maxPerLevel when positive.
func truncateTypeBucketsByID(buckets map[string][]graph.TypeOrigin, maxPerLevel int) []string {
	out := make([]string, 0, len(buckets))
	for k := range buckets {
		out = append(out, k)
	}
	sort.Strings(out)
	if maxPerLevel > 0 && len(out) > maxPerLevel {
		out = out[:maxPerLevel]
	}
	return out
}

// dedupOrigins returns a sorted, distinct slice of the input origins.
func dedupOrigins(origins []graph.TypeOrigin) []graph.TypeOrigin {
	if len(origins) == 0 {
		return nil
	}
	seen := map[graph.TypeOrigin]bool{}
	out := make([]graph.TypeOrigin, 0, len(origins))
	for _, o := range origins {
		if seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
