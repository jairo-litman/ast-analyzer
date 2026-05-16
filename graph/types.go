// Package graph turns a directory of TypeScript sources into an
// in-memory project graph: declared symbols, call sites, and resolved
// import edges between files.
package graph

import (
	"github.com/jairo-litman/ast-analyzer/extractor"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// SymbolKind tags what a Symbol represents in the source.
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolInterface SymbolKind = "interface"
	SymbolEnum      SymbolKind = "enum"
	SymbolTypeAlias SymbolKind = "type_alias"

	// SymbolModule is the synthetic per-file caller for call sites
	// that fall outside any function body. Emitted only for files
	// that contain at least one module-scope call.
	SymbolModule SymbolKind = "module"
)

// Symbol is one top-level named declaration anchored to a byte range
// in a source file. ID is unique within a Project.
//
// BodyStartByte points at the body subtree's first byte for
// function-kind symbols; slice [StartByte, BodyStartByte) for the
// declaration prefix. Zero for other kinds.
//
// ClassDetails / InterfaceDetails carry the structural data the
// resolver and pruner need without re-parsing. nil for other kinds.
//
// IsDefaultExport flags the file's `export default <decl|name>`
// target so default-import bindings can resolve to it.
type Symbol struct {
	ID               string            `json:"id"`
	Kind             SymbolKind        `json:"kind"`
	Name             string            `json:"name"`
	File             string            `json:"file"` // relative to Project.Root
	StartByte        uint              `json:"start_byte"`
	EndByte          uint              `json:"end_byte"`
	BodyStartByte    uint              `json:"body_start_byte,omitempty"`
	ClassDetails     *ClassDetails     `json:"class_details,omitempty"`
	InterfaceDetails *InterfaceDetails `json:"interface_details,omitempty"`
	IsDefaultExport  bool              `json:"is_default_export,omitempty"`

	// LocalTypes maps a local-variable name declared in a function-
	// kind symbol's body to the class name it holds, populated from
	// `const x = new T(...)` and `const x: T = ...` declarations.
	// `const x = factory()` entries are merged in by ResolveCalls
	// once the called function's ReturnType is known. Used to
	// resolve `x.method()` against T's class chain.
	LocalTypes map[string]string `json:"local_types,omitempty"`

	// LocalCallBindings maps a local-variable name to the bare
	// identifier of the function it was initialized from, for
	// declarations like `const x = factory();` or `const x = await
	// factory();`. ResolveCalls walks this in a pre-pass, looks up
	// `factory` against the file's scope, and populates LocalTypes
	// with `stripGenericArgs(factory.ReturnType)` when the lookup
	// succeeds.
	LocalCallBindings map[string]string `json:"local_call_bindings,omitempty"`

	// LocalMethodBindings maps a local-variable name to the
	// (receiver, method) pair that initialized it, for
	// `const x = receiver.method(...)` and the awaited form.
	// Resolved during enrichment by looking up receiver's type in
	// the same symbol's LocalTypes, finding method on that class,
	// and copying its stripped ReturnType into LocalTypes.
	// Iteration in the enrichment loop lets multi-hop chains
	// (`const a = fn(); const b = a.x(); const c = b.y()`) converge.
	LocalMethodBindings map[string]LocalMethodTarget `json:"local_method_bindings,omitempty"`

	// LocalDestructureBindings maps a local-variable name to the
	// destructuring source that produced it, for declarations like
	// `const { ctx, sut } = factory();` and the awaited /
	// method-on-receiver / renamed variants. Resolved during
	// enrichment by following the source call's return type to a
	// class or interface and looking up the destructured property.
	LocalDestructureBindings map[string]LocalDestructureSource `json:"local_destructure_bindings,omitempty"`

	// ReturnType is the function-kind symbol's declared return
	// type. Copied from the extractor's annotation when present;
	// otherwise inferred from a consistent `return new T(...)`
	// pattern in the body. Empty for non-function symbols and for
	// functions with neither annotation nor a confidently-inferable
	// return shape.
	ReturnType string `json:"return_type,omitempty"`

	// InlineReturnProperties is populated by the extractor when a
	// function lacks an explicit return type and returns an object
	// literal at top level. Maps property name → source spec used
	// at enrichment time to answer destructuring property lookups
	// (`const { a, b } = factory()`) without requiring the factory
	// to declare a named class/interface return type.
	InlineReturnProperties map[string]InlineReturnSource `json:"inline_return_properties,omitempty"`

	// LocalTypeOrigins tags each LocalTypes entry with the rule that
	// produced it.
	LocalTypeOrigins map[string]TypeOrigin `json:"local_type_origins,omitempty"`

	// TypeRefs is the resolved view of every type expression this
	// symbol references. Filled by populateTypeRefs after ResolveCalls.
	TypeRefs []SymbolTypeRef `json:"type_refs,omitempty"`
}

// SymbolTypeRef pairs a TypeRef with a slot tag identifying which
// part of the owning Symbol's declaration it came from. Slot values:
// "return", "param:<name>", "local:<name>", "extends", "extends:<i>",
// "implements:<i>", "property:<name>", "value".
type SymbolTypeRef struct {
	Slot string  `json:"slot"`
	Ref  TypeRef `json:"ref"`
}

// LocalMethodTarget describes a method-on-receiver initializer the
// resolver defers until enrichment time.
type LocalMethodTarget struct {
	Receiver string `json:"receiver"`
	Method   string `json:"method"`
}

// LocalDestructureSource describes one destructured local from
// `const { ...keys } = source(...)`. Receiver/Callee identify the
// source call (bare function when Receiver is empty, otherwise
// method-on-receiver). Property names the key being destructured —
// distinct from the local var name when the renaming form is used
// (`const { src: local } = ...`).
type LocalDestructureSource struct {
	Receiver string `json:"receiver,omitempty"`
	Callee   string `json:"callee"`
	Property string `json:"property"`
}

// InlineReturnSource describes one property of an object-literal
// returned by a function with no explicit return-type annotation.
// Exactly one of NewType / LocalVar is populated:
//   - NewType:  rhs was `new T(...)`; the property's type is T.
//   - LocalVar: rhs was a local-var identifier (`{ a }` shorthand
//     or `{ a: localVar }`); the property's type is the local's
//     LocalTypes entry on the owning function.
type InlineReturnSource struct {
	NewType  string `json:"new_type,omitempty"`
	LocalVar string `json:"local_var,omitempty"`
}

// ClassDetails carries enough structural data to render a class
// header (declaration line, properties, method signatures) without
// AST access. Round-tripped through the store as a JSON blob.
type ClassDetails struct {
	Abstract   bool                        `json:"abstract,omitempty"`
	Extends    string                      `json:"extends,omitempty"`
	Implements []string                    `json:"implements,omitempty"`
	Properties []extractor.ObjectProperty  `json:"properties,omitempty"`
	Methods    []extractor.MethodSignature `json:"methods,omitempty"`
}

// InterfaceDetails carries the structural data of an interface
// declaration: extends chain, properties, method signatures.
type InterfaceDetails struct {
	Extends    []string                    `json:"extends,omitempty"`
	Properties []extractor.ObjectProperty  `json:"properties,omitempty"`
	Methods    []extractor.MethodSignature `json:"methods,omitempty"`
}

// CallSite is one call_expression or new_expression. CallerID is the
// enclosing function-kind or module-kind Symbol's ID.
//
// ResolvedTo is populated by ResolveCalls with the IDs of Symbol(s)
// the call statically targets. May hold multiple entries when names
// collide (e.g. a function and a class sharing a name); empty for
// calls whose target isn't statically knowable.
type CallSite struct {
	CallerID      string   `json:"caller_id"`
	Callee        string   `json:"callee"`
	Receiver      string   `json:"receiver,omitempty"`
	Expression    string   `json:"expression"`
	IsConstructor bool     `json:"is_constructor,omitempty"`
	File          string   `json:"file"`
	StartByte     uint     `json:"start_byte"`
	EndByte       uint     `json:"end_byte"`
	ResolvedTo    []string `json:"resolved_to,omitempty"`
}

// ImportEdge is one import or require statement from a file.
// Resolved holds the project-relative path the resolver produced, or
// "" when the import couldn't be resolved.
//
// StartByte / EndByte mark the statement's byte range so the source
// can be reconstructed by slicing without holding an AST node.
type ImportEdge struct {
	File        string                        `json:"file"`
	Path        string                        `json:"path"`
	Resolved    string                        `json:"resolved,omitempty"`
	Kind        extractor.ImportKind          `json:"kind"`
	Namespace   string                        `json:"namespace,omitempty"`
	Identifiers []extractor.IdentifierContext `json:"identifiers,omitempty"`
	StartByte   uint                          `json:"start_byte"`
	EndByte     uint                          `json:"end_byte"`
}

// ReExportEdge is one `export ... from "..."` statement. Variants
// are encoded by which fields are populated:
//
//   - Bindings non-empty  → `export { a, b as c } from "..."`
//   - Namespace non-empty → `export * as ns from "..."`
//   - both empty          → `export * from "..."`
type ReExportEdge struct {
	File      string               `json:"file"`
	Path      string               `json:"path"`
	Resolved  string               `json:"resolved,omitempty"`
	Kind      extractor.ImportKind `json:"kind"`
	Namespace string               `json:"namespace,omitempty"`
	Bindings  []ReExportBinding    `json:"bindings,omitempty"`
	StartByte uint                 `json:"start_byte"`
	EndByte   uint                 `json:"end_byte"`
}

// ReExportBinding is one entry inside an `export { ... } from` clause.
type ReExportBinding struct {
	LocalName  string `json:"local_name"`
	RemoteName string `json:"remote_name"`
	IsTypeOnly bool   `json:"is_type_only,omitempty"`
}

// FileResult holds the per-file extractor output a Project keeps
// around so consumers can fetch raw FunctionContext / ClassContext /
// etc. by file path without re-parsing.
type FileResult struct {
	Path        string
	Imports     []extractor.ImportContext
	ReExports   []extractor.ReExportContext
	Functions   []extractor.FunctionContext
	Classes     []extractor.ClassContext
	Interfaces  []extractor.InterfaceContext
	Enums       []extractor.EnumContext
	TypeAliases []extractor.TypeAliasContext
}

// Project is the in-memory graph for a TypeScript codebase rooted at
// Root. The Project owns its parse trees; *sitter.Node pointers in
// FileResult are valid until Close is called.
type Project struct {
	Root      string                 // absolute path to project root
	Files     map[string]*FileResult // relative path → per-file extractor output
	Symbols   []Symbol
	Calls     []CallSite
	Imports   []ImportEdge
	ReExports []ReExportEdge

	// Warnings is populated during BuildProject / UpdateFiles when a
	// recoverable inconsistency is detected (e.g. multiple symbols
	// flagged as the same file's default export). Not persisted.
	Warnings []string

	// trees is keyed by project-relative path so RemoveFile can free
	// the parse tree of a replaced or deleted file without leaking.
	trees map[string]*sitter.Tree
}

// Close releases the parse trees backing FileResult's Node pointers.
// Calling Close on a tree-less Project (e.g. one from store.Load)
// is a no-op.
func (p *Project) Close() {
	for _, t := range p.trees {
		if t != nil {
			t.Close()
		}
	}
	p.trees = nil
}
