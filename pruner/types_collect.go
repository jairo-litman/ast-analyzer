package pruner

import (
	"sort"

	"github.com/jairo-litman/ast-analyzer/graph"
)

// collectTypesBFS walks the target's TypeRefs outward up to
// opts.TypeDepth hops, one TypeEntry per distinct referenced symbol.
// Cycles terminate via a visited set seeded with the target.
func collectTypesBFS(p *graph.Project, cache *sourceCache, target graph.Symbol, opts ExtractOptions) ([]TypeEntry, error) {
	if opts.TypeDepth <= 0 {
		return nil, nil
	}

	visited := map[string]bool{target.ID: true}
	entriesByID := map[string]*TypeEntry{}
	var order []string

	frontier := []string{target.ID}

	for depth := 1; depth <= opts.TypeDepth && len(frontier) > 0; depth++ {
		buckets := map[string][]graph.TypeOrigin{}
		for _, currentID := range frontier {
			sym, ok := lookupSymbol(p, currentID)
			if !ok {
				continue
			}
			for _, sref := range sym.TypeRefs {
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
