// Package pruner assembles the structured context payload for a
// target symbol: its full body, the enclosing class header (when
// applicable), the file's relevant imports, and N-hop callers and
// callees from the call graph (depth and body inclusion are
// configurable via ExtractOptions).
package pruner

import "github.com/jairo-litman/ast-analyzer/graph"

// ExtractOptions controls how far the BFS over the call graph
// walks and which entries carry full source vs declaration-only
// signature.
type ExtractOptions struct {
	// CallerDepth: 0 = no callers; 1 = direct callers (default);
	// N = walk up to N hops upward in the call graph.
	CallerDepth int
	// CalleeDepth: 0 = no callees; 1 = direct callees (default);
	// N = walk up to N hops downward in the call graph.
	CalleeDepth int

	// CallerBodyDepth: for callers at depth <= this, populate Body
	// with the full source slice. Beyond, only Signature is set.
	// 0 (default) = signatures only.
	CallerBodyDepth int
	// CalleeBodyDepth: same semantics, for callees.
	CalleeBodyDepth int

	// MaxPerLevel caps the number of entries retained at each BFS
	// level (after deterministic sorting by symbol ID). 0 = no cap.
	MaxPerLevel int

	// TypeDepth: BFS depth over the type-reference graph (0 = none,
	// 1 = types directly referenced by the target, N = N hops).
	// Default 0.
	TypeDepth int
}

// DefaultExtractOptions returns single-hop callers and callees with
// full bodies, no type expansion, and a soft cap of 50 per level.
func DefaultExtractOptions() ExtractOptions {
	return ExtractOptions{
		CallerDepth:     1,
		CalleeDepth:     1,
		CallerBodyDepth: 1,
		CalleeBodyDepth: 1,
		MaxPerLevel:     50,
	}
}

// Context is the structured payload returned for one Extract request.
type Context struct {
	// Target is the requested symbol's full source.
	Target TargetSource

	// EnclosingType carries the surrounding class declaration when the
	// target is a class method.
	EnclosingType *EnclosingType

	// Imports is every relevant import statement from the target's
	// file, in source order.
	Imports []ImportEntry

	// Callees and Callers are deduplicated by Symbol; each entry's
	// CallSites slice lists the relevant call expressions. Depth
	// records BFS distance from the target (1 = direct).
	Callees []Callee
	Callers []Caller

	// Types is the deduplicated set of type-bearing symbols reachable
	// from the target's TypeRefs within ExtractOptions.TypeDepth hops.
	Types []TypeEntry

	// ImportChains records, for each file (other than the target's
	// own) that references the target, the trail of import and
	// re-export statements that bring the target's name into that
	// file's scope.
	ImportChains []ImportChain
}

// ImportChain is the trail of import / re-export hops that connect
// an importing file to the target symbol. LocalName is the name as
// it appears in the importing file's source; TargetName is the
// canonical name at the declaration site.
type ImportChain struct {
	TargetSymbolID string
	TargetName     string
	LocalName      string
	ImportingFile  string
	Trail          []ChainHop
}

// ChainHop is one statement on an ImportChain. Kind is "import" for
// the entry in the importing file, "re-export" for every subsequent
// hop until the declaration site is reached.
type ChainHop struct {
	File      string
	Kind      string
	Source    string
	StartByte uint
	EndByte   uint
}

// TypeEntry is one type symbol surfaced by the TypeDepth BFS.
// Origins aggregates every TypeOrigin under which the type appeared.
type TypeEntry struct {
	Symbol  graph.Symbol
	Source  string
	File    string
	Depth   int
	Origins []graph.TypeOrigin
}

// TargetSource is the requested symbol plus its full source text.
type TargetSource struct {
	Symbol graph.Symbol
	Source string
}

// EnclosingType is the class lexically containing a method target.
// Source renders as a stripped header with method bodies elided.
type EnclosingType struct {
	Symbol graph.Symbol
	Source string
}

// ImportEntry pairs an ImportEdge with the raw source text of its
// statement.
type ImportEntry struct {
	Edge   graph.ImportEdge
	Source string
}

// Callee is one function reachable from the target via the call
// graph. Signature is always the declaration prefix; Body is the
// full source slice, populated only when this entry's Depth is
// within CalleeBodyDepth. CallSites are the calls that pulled this
// callee into the context.
type Callee struct {
	Symbol    graph.Symbol
	Signature string
	Body      string
	File      string
	Depth     int
	CallSites []graph.CallSite
}

// Caller is one function (or module scope) that reaches the target
// via the call graph. Body is populated only when Depth is within
// CallerBodyDepth. CallSites are the calls that pulled this caller
// into the context.
type Caller struct {
	Symbol    graph.Symbol
	Signature string
	Body      string
	File      string
	Depth     int
	CallSites []graph.CallSite
}
