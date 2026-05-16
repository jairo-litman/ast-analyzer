# ast-analyzer

> Static-analysis engine for **AST-driven context extraction** targeting
> Large Language Models.

This repository is the implementation track of the TCC project
**"Extração de contexto otimizado para LLMs utilizando análise estática
e grafos de relacionamento baseados em AST"** (UNESP / FC Bauru, 2026).

The goal is to replace the "dump the whole file into the prompt"
heuristic with a surgical extractor: given a target symbol in a
TypeScript repository, return only the symbol's body, its enclosing
class header (when applicable), the signatures of its callers and
callees, the declarations of the types it references (explicit
annotations and inferred), and the file's imports — driven by a
tree-sitter parse tree and an inter-procedural call graph augmented
with a type-reference graph.

## Quick start

Build the binary, then point it at a TypeScript project that has a
`tsconfig.json`:

```sh
go build -o astanalyzer ./cmd/astanalyzer

# 1. index the project once: walks the tree, parses every .ts/.tsx
#    file in parallel, persists the graph to SQLite. Re-runs are
#    incremental — only files whose content hash changed get re-parsed.
./astanalyzer index --tsconfig path/to/tsconfig.json path/to/project

# 2. enumerate every symbol the analyzer found
./astanalyzer list path/to/project

# 3. emit a context payload (json | redacted | markdown)
./astanalyzer extract --format redacted path/to/project 'src/foo.ts#123'

# 4. (optional) keep the index live as you edit
./astanalyzer watch --tsconfig path/to/tsconfig.json path/to/project
```

The default index path is `<root>/.astanalyzer/index.db` for all four
commands; override with `--db <path>` (or `--output <path>` for
`index`). Pass `--rebuild --tsconfig <path>` to `list`/`extract` if
you want an in-memory build with no persisted index.

`list` and `extract` warn on stderr when the on-disk source has
drifted from the indexed snapshot. Pass `--no-stale-check` to suppress
the warning if you know the index is fresh. Build-time warnings (e.g.
multiple symbols flagged as default-export) are also emitted on stderr
during `index`, `watch`, and any `--rebuild` path.

`list` accepts `--kind <comma,list>`, `--file <regex>`, and
`--name <regex>` filters; all combine as AND. Useful for narrowing a
large project's symbol catalog before piping into anything else.

`extract`'s output formats:
- **json** — the structured `Context` payload.
- **redacted** — multi-file source-faithful view with `<- cut content ->`
  markers between kept regions. Each file's section is prefixed with
  a `// callees: …` / `// callers: …` / `// types: name (depth=N, kind, origins=…)`
  metadata comment summarizing what was pulled in. Doc comments
  (`/** … */` or runs of `//` lines) above kept declarations travel
  with the source.
- **markdown** — same algorithm as redacted, wrapped in `## file:`
  headings with fenced `ts` code blocks. Prompt-ready.

Symbol IDs are file-and-position keyed (`<relpath>#<startByte>`, or
`<relpath>#module` for the synthetic module-scope caller). `list` is
the canonical way to discover them.

**Tuning the call-graph and type-graph slice.** By default `extract`
returns direct callers and direct callees with their full bodies and
no type declarations. Six flags shape the slice; defaults reproduce
the legacy behavior, so existing invocations need no changes:

- `--caller-depth N` / `--callee-depth N` — BFS depth in each
  direction over the call graph (0 = none, 1 = direct, N = N hops).
  Default 1. Cycles and self-calls terminate via a visited set
  seeded with the target.
- `--caller-bodies-up-to N` / `--callee-bodies-up-to N` — full-body
  inclusion cap. Entries at depth within this number carry their
  full source; deeper entries reduce to the declaration line
  (`function foo(x: T): R`) with the body bytes elided. Default 1.
- `--type-depth N` — BFS depth over the type-reference graph
  (0 = none, 1 = types directly referenced by the target, N = N
  hops). On a function target the direct references are parameter
  types, the return type, and local-variable types; on a class
  target they are `extends`, every `implements` entry, and every
  typed property; on an interface target they are the extends chain
  and properties; on a type alias they are the identifiers parsed
  out of the RHS. Each type entry carries its inference origin
  (`annotation`, `inferred:new`, `inferred:call-return`,
  `inferred:method-return`, `inferred:destructure`,
  `inferred:object-literal`). Default 0.
- `--max-per-level N` — soft cap on entries kept at each BFS level,
  applied after a deterministic sort by symbol ID. Default 50; pass
  `0` for no cap.

Example: pull two levels of callees with bodies on the direct hop
and signatures on the transitive one, plus the immediate type
references of the target:

```sh
./astanalyzer extract \
    --callee-depth 2 --callee-bodies-up-to 1 \
    --type-depth 1 \
    --format markdown $ROOT 'src/services/api.ts#95'
```

## Showcase

A self-contained TypeScript fixture lives at `graph/testdata/full/` —
a small Todo app with `tsconfig.json` paths (`@models/*`,
`@services/*`, `@utils/*`), a class-inheritance chain (`BaseTodo` →
`Todo`), interfaces, enums, type aliases, a default-export class, and
re-export barrel files. It exercises every capability in the codebase.
Build the binary and index the fixture first:

```sh
go build -o astanalyzer ./cmd/astanalyzer
ROOT=graph/testdata/full
./astanalyzer index --tsconfig $ROOT/tsconfig.json $ROOT
```

**Discover symbols.** Lists every declaration the analyzer found
along with its symbol ID, kind, and file:

```sh
./astanalyzer list $ROOT
./astanalyzer list --kind class,interface --file 'models/' $ROOT
```

**Extract a function with cross-file callees and callers.** Shows the
target body, its file's imports (filtered to those actually
referenced), the `Storage`/`Todo` class headers pulled in via the
call graph, and the `main()` caller. Resolver coverage hit by this
target alone: `tsconfig` path aliases (`@models/...`), default
imports, `this`/`super` against the class chain with interface-chain
fallback, locally-typed instance method calls (`storage.save(...)`),
and re-export chains in their named, aliased, default, namespace, and
star variants:

```sh
./astanalyzer extract --format redacted $ROOT 'src/services/api.ts#95'   # createTodo
```

**Extract a class method.** Same flow, but the target is inside a
class — the redacted view emits the enclosing class header with
sibling methods elided as `<- cut content ->`:

```sh
./astanalyzer extract --format redacted $ROOT 'src/services/storage.ts#153'  # Storage.save
```

**Extract a non-function symbol.** `Extract` accepts class /
interface / enum / type-alias targets too; callees stay empty,
callers are the `new T(...)` / type-reference sites:

```sh
./astanalyzer extract --format redacted $ROOT 'src/services/storage.ts#53'  # Storage class (default export)
./astanalyzer extract --format json     $ROOT 'src/models/types.ts#145'     # TodoLike interface
```

**Pull a function with its referenced type declarations.** Adding
`--type-depth N` follows the type-reference graph N hops from the
target. Depth 1 surfaces every type that appears directly in the
target's signature, return position, or local-variable bindings;
depth 2 also pulls the types those types reference (extends /
implements / property types):

```sh
./astanalyzer extract --format redacted --type-depth 1 $ROOT 'src/services/api.ts#95'
./astanalyzer extract --format markdown --type-depth 2 $ROOT 'src/services/storage.ts#153'
```

**Render markdown for an LLM prompt.** Same content as redacted, but
with `## file:` headings and fenced `ts` blocks ready to drop into a
chat:

```sh
./astanalyzer extract --format markdown $ROOT 'src/services/api.ts#95'
```

**Re-index after edits.** A second `index` run hashes each file,
re-parses only the ones that changed, and reports per-file counts:

```sh
./astanalyzer index --tsconfig $ROOT/tsconfig.json $ROOT
# files: 0 added, 0 changed, 0 removed, 7 unchanged
```

**Keep the index live.** `watch` runs an initial pass and then
re-indexes on every edit, debounced to coalesce bursts. Ctrl-C exits
cleanly:

```sh
./astanalyzer watch --tsconfig $ROOT/tsconfig.json $ROOT
# indexed: 7 added, 0 changed, 0 removed, 0 unchanged
# (edit a file)
# indexed: 0 added, 1 changed, 0 removed, 6 unchanged
```

## Layout

```
extractor/
  extractor.go          – Extractor type, language + query catalog wiring
                          (TS and TSX grammar variants)
  parser.go             – source → tree-sitter tree
  query.go              – runQuery / matchView helpers shared by all queries
  imports.go            – ImportContext + QueryImports
  re_exports.go         – ReExportContext + QueryReExports
  default_exports.go    – DefaultExportContext + QueryDefaultExports
  functions.go          – FunctionContext + QueryFunctions
  function_calls.go     – FunctionCallContext + QueryFunctionCalls
  classes.go            – ClassContext + QueryClasses
  interfaces.go         – InterfaceContext + QueryInterfaces
  enums.go              – EnumContext + QueryEnums
  type_aliases.go       – TypeAliasContext + QueryTypeAliases
  objects.go            – shared ObjectProperty / MethodSignature types
  resolver.go           – tsconfig-aware import resolver with caching FS;
                          handles tsconfig path aliases, package.json's
                          `main` / `types` / `exports` (incl. subpath
                          glob patterns), and `#name` imports.
  queries/              – embedded tree-sitter .scm query files
  testdata/resolver     – on-disk fixture for the resolver / JSONC tests
graph/
  types.go              – Symbol (incl. LocalTypes, LocalCallBindings,
                          LocalMethodBindings, LocalDestructureBindings,
                          LocalTypeOrigins, InlineReturnProperties,
                          ReturnType, TypeRefs), SymbolTypeRef, CallSite,
                          ImportEdge, ReExportEdge, Project (incl.
                          build-time Warnings)
  typeref.go            – TypeRef shape, TypeOrigin enum, ParseTypeRef
                          parser covering bare/qualified identifiers,
                          generics, arrays, unions, intersections;
                          WalkBaseTypes / IsPrimitive helpers
  typerefs.go           – populateTypeRefs: emits the unified
                          SymbolTypeRef view on every kind (function
                          params/return/locals, class extends/implements/
                          properties, interface extends/properties, type
                          alias RHS) and resolves each ref's BaseName to
                          project Symbol IDs via the call resolver's
                          scope walker
  build.go              – BuildProject: parallel parse phase across a
                          runtime.NumCPU() worker pool, serial merge
                          into the Project, deterministic output sorting.
                          Houses IsSkippedDir / IsIncludedFile, decorator-
                          aware method symbol ranges, multi-default
                          warning emission. Local-binding extraction
                          (LocalCallBindings, LocalMethodBindings,
                          LocalDestructureBindings) and inline-return
                          inference live here too.
  resolve.go            – ResolveCalls: cross-file callee resolution
                          covering named / aliased / default / namespace
                          imports, every re-export shape (named, aliased,
                          star, default `{ default as X }`, namespace
                          `* as ns`), this/super against the class chain
                          with interface fallback, super() constructor,
                          static methods (`ClassName.method()`), local-
                          instance member calls (`x.method()` for `x`
                          bound via `new T(...)`, `: T`, factory return,
                          method-on-receiver return, destructuring),
                          this.<field>.<method>() chains of arbitrary
                          depth, and Promise<T> unwrapping. Followed by
                          populateTypeRefs.
  incremental.go        – HashSourceFiles, RemoveFile, UpdateFiles for
                          partial re-indexing
  testdata/             – fixtures: simple, full, inheritance, module,
                          reexport, resolution, default_export, tsx,
                          decorators, imports_filter, local_instance,
                          multi_default, call_chain (BFS depth + cycles),
                          docs (doc-comment preservation),
                          module_caller (module-scope context expansion),
                          static_method, this_field, this_chain,
                          factory_return, factory_method, destructure,
                          inferred_returns, typerefs (type indexing)
store/
  store.go              – Open / schema / Close (incl. files.content_hash)
  save.go               – Save / SaveWithHashes (full rebuild + per-file hashes)
  load.go               – Load / LoadFileHashes
pruner/
  types.go              – Context, TargetSource, EnclosingType, Callee, Caller,
                          TypeEntry (with Body / Depth / Origins fields),
                          ExtractOptions (incl. TypeDepth) /
                          DefaultExtractOptions
  extract.go            – Extract (legacy wrapper) + ExtractWithOptions;
                          BFS over the call graph in both directions with
                          visited-set cycle protection, per-level MaxPerLevel
                          truncation, and body/signature mode capped by
                          {Caller,Callee}BodyDepth; import relevance filter
  types_collect.go      – collectTypesBFS: BFS over the type-reference
                          graph from the target outward up to TypeDepth
                          hops, aggregating origins per surfaced type
  render.go             – computeFileSections shared between RenderRedacted
                          and RenderMarkdown; per-file metadata comment
                          listing kept callees / callers / types with depth
                          (and origin set for types); doc-comment
                          preservation (JSDoc / `//` runs above
                          declarations, walking past `export`/`const`/
                          `abstract`/... modifiers); whitespace-only range
                          collapse around cut markers; context-window
                          expansion for module-scope call sites so test
                          blocks (`describe(...)` / `it(...)`) render with
                          their enclosing structure
cli/
  cli.go                – Run dispatcher and subcommand registry
  project.go            – DB-first dispatch: defaultDBPath, projectHandle,
                          loadProjectFromDB, rebuildProject, warnIfStale,
                          emitWarnings
  list.go               – DB-first listing with --kind / --file / --name
                          filters and the --no-stale-check toggle
  extract.go            – DB-first extraction with the same toggles plus
                          --format json|redacted|markdown
  index.go              – incremental indexing: walk + hash + diff + apply
  watch.go              – fsnotify-driven watch loop with debounced re-index
cmd/astanalyzer/
  main.go               – binary entry point; delegates to cli.Run
jsonc/                  – tolerant JSONC reader (adapted, MIT — used to
                          parse tsconfig.json with comments)
```

Each new query type lands as a sibling of `imports.go` /
`functions.go`: a struct describing the extracted shape, a `Query…`
method on `*Extractor`, and one or more `.scm` files under
`extractor/queries/`. The shared helpers in `query.go` keep the
boilerplate to the per-capture switch.

## Build and test

Requires Go 1.26+ (matching `go.mod`) and a working CGO toolchain — the
`go-tree-sitter` and `go-sqlite3` bindings build C sources.

```sh
go build ./...
go test ./...
```

Tests are written TDD-style. Per-extractor coverage lives in row-per-
behavior table-driven tests next to each query (`extractor/imports_test.go`,
`extractor/functions_test.go`, …); higher layers (`graph`, `store`,
`pruner`, `cli`) drive end-to-end flows against synthetic fixture
projects under their respective `testdata/` directories. New behavior
should land as a failing test first.

## License

MIT — see [`LICENSE`](LICENSE). The `jsonc/` package is adapted from
<https://github.com/muhammadmuzzammil1998/jsonc> (MIT); that upstream
notice is preserved in the package's source files.
