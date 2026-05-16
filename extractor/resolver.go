package extractor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jairo-litman/ast-analyzer/jsonc"
)

// FileSystem is the resolver's I/O indirection. Stat returns both
// bits in one call so CachingFS can issue at most one underlying stat
// per path.
type FileSystem interface {
	Stat(path string) (isFile, isDir bool)
	Read(path string) ([]byte, error)
}

type OSFileSystem struct{}

// Stat treats anything that isn't a directory as a regular file —
// symlinks to source files resolve like the targets they point at.
func (OSFileSystem) Stat(path string) (isFile, isDir bool) {
	info, err := os.Stat(path)
	if err != nil {
		return false, false
	}
	if info.IsDir() {
		return false, true
	}
	return true, false
}

func (OSFileSystem) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

type statResult struct{ isFile, isDir bool }

// CachingFS memoizes Stat lookups. Safe for concurrent use.
type CachingFS struct {
	underlying FileSystem
	cache      map[string]statResult
	mu         sync.RWMutex
}

func NewCachingFS(fs FileSystem) *CachingFS {
	return &CachingFS{
		underlying: fs,
		cache:      make(map[string]statResult),
	}
}

func (c *CachingFS) Stat(path string) (isFile, isDir bool) {
	c.mu.RLock()
	r, ok := c.cache[path]
	c.mu.RUnlock()
	if ok {
		return r.isFile, r.isDir
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.cache[path]; ok {
		return r.isFile, r.isDir
	}

	isFile, isDir = c.underlying.Stat(path)
	c.cache[path] = statResult{isFile, isDir}
	return isFile, isDir
}

func (c *CachingFS) Read(path string) ([]byte, error) {
	return c.underlying.Read(path)
}

type ResolverConfig struct {
	BaseDir string
	Paths   map[string][]string
}

type aliasTarget struct {
	prefix  string
	suffix  string
	isExact bool
}

type aliasEntry struct {
	patternPrefix string
	patternSuffix string
	isExact       bool
	originalLen   int
	targets       []aliasTarget
}

type Resolver struct {
	config  *ResolverConfig
	fs      FileSystem
	aliases []aliasEntry
}

func NewResolver(config *ResolverConfig, fs FileSystem) *Resolver {
	// Alias-resolved imports must come back absolute so consumers
	// joining them against an absolute project root (filepath.Rel)
	// don't silently drop the resolution.
	if config.BaseDir != "" && !filepath.IsAbs(config.BaseDir) {
		if abs, err := filepath.Abs(config.BaseDir); err == nil {
			config.BaseDir = abs
		}
	}

	var aliases []aliasEntry

	// Pre-compute alias wildcards
	for pattern, targets := range config.Paths {
		parts := strings.SplitN(pattern, "*", 2)
		entry := aliasEntry{
			isExact:     len(parts) == 1,
			originalLen: len(pattern),
		}

		if entry.isExact {
			entry.patternPrefix = pattern
		} else {
			entry.patternPrefix = parts[0]
			entry.patternSuffix = parts[1]
		}

		for _, t := range targets {
			tParts := strings.SplitN(t, "*", 2)
			target := aliasTarget{isExact: len(tParts) == 1}
			if target.isExact {
				target.prefix = t
			} else {
				target.prefix = tParts[0]
				target.suffix = tParts[1]
			}
			entry.targets = append(entry.targets, target)
		}
		aliases = append(aliases, entry)
	}

	// Sort by length of the original pattern (longest first)
	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].originalLen > aliases[j].originalLen
	})

	return &Resolver{
		config:  config,
		fs:      NewCachingFS(fs),
		aliases: aliases,
	}
}

func NewResolverFromConfigPath(configPath string) (*Resolver, error) {
	config, err := parseConfigChain(OSFileSystem{}, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse resolver config: %w", err)
	}
	return NewResolver(config, OSFileSystem{}), nil
}

// rawConfig is one unmerged tsconfig.json. BaseUrl stays unresolved
// so the merge layer can decide whether to honor it (child override)
// or inherit the parent's already-resolved BaseDir.
type rawConfig struct {
	Extends string
	BaseUrl string
	Paths   map[string][]string
	Path    string // path of the file itself, cleaned for stable cycle keys
}

// parseConfigChain reads the tsconfig at path and merges its
// `extends` chain into a single ResolverConfig. Child paths win over
// parents on key collision; a child's baseUrl wins when set,
// otherwise the closest ancestor's resolved BaseDir is inherited.
//
// Failure to read or parse the entry-point file is fatal. Errors
// further up the chain terminate the walk and return the partial
// merge rather than failing the whole build. Cycles are detected
// and broken; each file is visited at most once.
func parseConfigChain(fs FileSystem, path string) (*ResolverConfig, error) {
	raw, err := parseConfigSingle(fs, path)
	if err != nil {
		return nil, err
	}
	visited := map[string]bool{raw.Path: true}
	return resolveConfigFromRaw(fs, raw, visited), nil
}

// parseConfigSingle reads one tsconfig.json without following its
// `extends` field.
func parseConfigSingle(fs FileSystem, path string) (*rawConfig, error) {
	clean := filepath.Clean(path)
	file, err := fs.Read(clean)
	if err != nil {
		return nil, fmt.Errorf("failed to read config %q: %w", clean, err)
	}

	var jsonData struct {
		Extends         string `json:"extends,omitempty"`
		CompilerOptions struct {
			BaseUrl string              `json:"baseUrl,omitempty"`
			Paths   map[string][]string `json:"paths,omitempty"`
		} `json:"compilerOptions"`
	}
	if err := jsonc.Unmarshal(file, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to parse jsonc in %q: %w", clean, err)
	}

	return &rawConfig{
		Extends: jsonData.Extends,
		BaseUrl: jsonData.CompilerOptions.BaseUrl,
		Paths:   jsonData.CompilerOptions.Paths,
		Path:    clean,
	}, nil
}

// resolveConfigFromRaw recursively merges raw's `extends` chain. raw
// must already be present in visited.
func resolveConfigFromRaw(fs FileSystem, raw *rawConfig, visited map[string]bool) *ResolverConfig {
	if raw.Extends == "" {
		return rawToResolverConfig(raw)
	}

	parentPath, err := resolveExtendsPath(fs, raw.Extends, filepath.Dir(raw.Path))
	if err != nil {
		return rawToResolverConfig(raw)
	}

	parentRaw, err := parseConfigSingle(fs, parentPath)
	if err != nil {
		return rawToResolverConfig(raw)
	}

	if visited[parentRaw.Path] {
		// Cycle: merge the parent once and stop following its extends.
		return mergeIntoChild(rawToResolverConfig(parentRaw), raw)
	}
	visited[parentRaw.Path] = true

	parentMerged := resolveConfigFromRaw(fs, parentRaw, visited)
	return mergeIntoChild(parentMerged, raw)
}

// rawToResolverConfig produces the merged shape of a single config
// with no inheritance applied — the leaf of any extends chain.
func rawToResolverConfig(raw *rawConfig) *ResolverConfig {
	paths := raw.Paths
	if paths == nil {
		paths = map[string][]string{}
	}
	return &ResolverConfig{
		BaseDir: resolveBaseDir(raw),
		Paths:   paths,
	}
}

// resolveBaseDir converts a rawConfig's BaseUrl to a BaseDir,
// preserving relative-or-absolute form. Empty BaseUrl falls back to
// the config file's own directory.
func resolveBaseDir(raw *rawConfig) string {
	configDir := filepath.Dir(raw.Path)
	if raw.BaseUrl == "" {
		return configDir
	}
	if filepath.IsAbs(raw.BaseUrl) {
		return raw.BaseUrl
	}
	return filepath.Join(configDir, filepath.FromSlash(raw.BaseUrl))
}

// mergeIntoChild merges parent into child. Child wins on baseUrl when
// set, and on any path key it declares; otherwise parent's data is
// inherited.
func mergeIntoChild(parent *ResolverConfig, child *rawConfig) *ResolverConfig {
	merged := &ResolverConfig{Paths: map[string][]string{}}

	if child.BaseUrl != "" {
		merged.BaseDir = resolveBaseDir(child)
	} else {
		merged.BaseDir = parent.BaseDir
	}

	for k, v := range parent.Paths {
		merged.Paths[k] = v
	}
	for k, v := range child.Paths {
		merged.Paths[k] = v
	}
	return merged
}

// resolveExtendsPath turns a raw `extends` value into a concrete
// path. Relative forms are joined to currentDir; absolute forms are
// used as-is; module-shaped forms are looked up under each ancestor's
// node_modules. `.json` is appended when missing.
func resolveExtendsPath(fs FileSystem, extends, currentDir string) (string, error) {
	if extends == "" {
		return "", fmt.Errorf("empty extends value")
	}
	if !strings.HasSuffix(extends, ".json") {
		extends = extends + ".json"
	}

	var candidates []string
	switch {
	case strings.HasPrefix(extends, "./") || strings.HasPrefix(extends, "../"):
		candidates = append(candidates, filepath.Join(currentDir, filepath.FromSlash(extends)))
	case filepath.IsAbs(extends):
		candidates = append(candidates, filepath.Clean(extends))
	default:
		// Module-shaped: walk parent dirs probing node_modules.
		dir := currentDir
		for {
			candidates = append(candidates, filepath.Join(dir, "node_modules", filepath.FromSlash(extends)))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	for _, c := range candidates {
		if isFile, _ := fs.Stat(c); isFile {
			return c, nil
		}
	}
	return "", fmt.Errorf("extends %q not found from %s", extends, currentDir)
}

func (r *Resolver) Resolve(importPath string, sourceFile string) (string, error) {
	if isRelative(importPath) {
		candidate := filepath.Join(filepath.Dir(sourceFile), filepath.FromSlash(importPath))
		if resolved, err := r.attemptResolve(candidate); err == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("unable to resolve relative import: %s", importPath)
	}

	if strings.HasPrefix(importPath, "#") {
		return r.resolveSubpathImport(importPath, sourceFile)
	}

	for _, alias := range r.aliases {
		if matchAlias(importPath, alias) {
			for _, target := range alias.targets {
				candidate := applyAlias(r.config.BaseDir, importPath, alias, target)
				if resolved, err := r.attemptResolve(candidate); err == nil {
					return resolved, nil
				}
			}
		}
	}

	return r.resolveNodeModule(importPath, sourceFile)
}

// resolveSubpathImport implements `#name` specifiers: walks up from
// sourceFile probing for a package.json that declares an `imports`
// field with a matching entry. Exact matches win over glob patterns,
// glob keys with longer literal prefixes win over shorter ones.
func (r *Resolver) resolveSubpathImport(importPath, sourceFile string) (string, error) {
	dir := filepath.Dir(sourceFile)
	for {
		pkgJSONPath := filepath.Join(dir, "package.json")
		if isFile, _ := r.fs.Stat(pkgJSONPath); isFile {
			if pkg, err := parsePackageJSON(r.fs, pkgJSONPath); err == nil && len(pkg.Imports) > 0 {
				if target, ok := resolveImportsField(pkg.Imports, importPath); ok {
					candidate := filepath.Join(dir, filepath.FromSlash(target))
					if resolved, err := r.attemptResolve(candidate); err == nil {
						return resolved, nil
					}
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("unable to resolve subpath import: %s", importPath)
}

// resolveImportsField is the imports-field analogue of
// resolveExportsField. It accepts only object values: a flat string
// at the top is not a valid `imports` shape.
func resolveImportsField(raw json.RawMessage, importPath string) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	if val, ok := obj[importPath]; ok {
		s := resolveExportsValue(val)
		return s, s != ""
	}
	return matchSubpathPattern(obj, importPath)
}

func matchAlias(importPath string, alias aliasEntry) bool {
	if alias.isExact {
		return importPath == alias.patternPrefix
	}
	return strings.HasPrefix(importPath, alias.patternPrefix) &&
		strings.HasSuffix(importPath, alias.patternSuffix)
}

func applyAlias(baseDir, importPath string, alias aliasEntry, target aliasTarget) string {
	if alias.isExact || target.isExact {
		return filepath.Join(baseDir, filepath.FromSlash(target.prefix))
	}

	// Extract content matched by the wildcard
	matchedContent := importPath[len(alias.patternPrefix) : len(importPath)-len(alias.patternSuffix)]
	replacedTarget := target.prefix + matchedContent + target.suffix

	return filepath.Join(baseDir, filepath.FromSlash(replacedTarget))
}

func isRelative(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// resolveNodeModule walks parent directories, probing each
// node_modules for importPath. At each step it consults the package's
// `types` / `main` / `exports` fields before falling back to direct
// file probing under the package root.
//
// `exports` is strict: when a package declares it and the subpath
// isn't advertised, no fallback is attempted under that package root.
// The walk still continues upward in case a different copy lives
// higher up the tree.
func (r *Resolver) resolveNodeModule(importPath, sourceFile string) (string, error) {
	dir := filepath.Dir(sourceFile)
	pkgName := topLevelPackageName(importPath)
	subpath := packageSubpath(importPath, pkgName)

	// Compute the colocated-index candidate suffix once outside the loop.
	moduleSuffix := filepath.Join("node_modules", filepath.FromSlash(importPath))

	for {
		pkgRoot := filepath.Join(dir, "node_modules", filepath.FromSlash(pkgName))
		pkgJSONPath := filepath.Join(pkgRoot, "package.json")

		var pkg *packageJSON
		if isFile, _ := r.fs.Stat(pkgJSONPath); isFile {
			if parsed, err := parsePackageJSON(r.fs, pkgJSONPath); err == nil {
				pkg = parsed
			}
		}

		if pkg != nil {
			if resolved, ok := r.resolveViaPackageJSON(pkgRoot, subpath, pkg); ok {
				return resolved, nil
			}
			// `exports` strict mode forbids the colocated-index
			// fallback under this pkgRoot.
			if !pkg.HasExports() {
				candidate := filepath.Join(dir, moduleSuffix)
				if resolved, err := r.attemptResolve(candidate); err == nil {
					return resolved, nil
				}
			}
		} else {
			candidate := filepath.Join(dir, moduleSuffix)
			if resolved, err := r.attemptResolve(candidate); err == nil {
				return resolved, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("unable to resolve import path: %s", importPath)
}

// topLevelPackageName returns the npm package name from an import
// specifier (`lodash` from `lodash/fp`, `@types/node` from
// `@types/node/path`).
func topLevelPackageName(importPath string) string {
	if strings.HasPrefix(importPath, "@") {
		// Scoped: @scope/name[/subpath]
		parts := strings.SplitN(importPath, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return parts[0]
	}
	if i := strings.IndexByte(importPath, '/'); i > 0 {
		return importPath[:i]
	}
	return importPath
}

// packageSubpath returns the `.`-prefixed subpath that an import
// requests, in the form `exports` keys use. Bare-package imports
// yield ".".
func packageSubpath(importPath, pkgName string) string {
	rest := strings.TrimPrefix(importPath, pkgName)
	if rest == "" {
		return "."
	}
	return "." + rest
}

// directExtensions are appended to candidates when probing for a
// concrete source file. The empty entry matches paths that already
// carry their extension.
var directExtensions = []string{
	"",
	".ts",
	".tsx",
	".d.ts",
	".cts",
	".mts",
	".js",
	".jsx",
	".cjs",
	".mjs",
}

// indexNames are tried inside a candidate directory when probing for
// an index entry. Order mirrors directExtensions.
var indexNames = []string{
	"index.ts",
	"index.tsx",
	"index.d.ts",
	"index.cts",
	"index.mts",
	"index.js",
	"index.jsx",
	"index.cjs",
	"index.mjs",
}

func (r *Resolver) attemptResolve(candidate string) (string, error) {
	for _, ext := range directExtensions {
		tryPath := candidate + ext
		if isFile, _ := r.fs.Stat(tryPath); isFile {
			return tryPath, nil
		}
	}

	// Skip index probes unless candidate is itself a directory.
	if _, isDir := r.fs.Stat(candidate); !isDir {
		return "", fmt.Errorf("file not found: %s", candidate)
	}
	for _, name := range indexNames {
		tryPath := filepath.Join(candidate, name)
		if isFile, _ := r.fs.Stat(tryPath); isFile {
			return tryPath, nil
		}
	}

	return "", fmt.Errorf("file not found: %s", candidate)
}

// packageJSON captures the subset of package.json fields the
// resolver needs. Exports / Imports stay as raw JSON because their
// values can be a string or a structured object.
type packageJSON struct {
	Main    string          `json:"main"`
	Types   string          `json:"types"`
	Typings string          `json:"typings"`
	Exports json.RawMessage `json:"exports"`
	Imports json.RawMessage `json:"imports"`
}

// HasExports reports whether the manifest declares a non-null
// `exports` field.
func (p *packageJSON) HasExports() bool {
	if len(p.Exports) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(p.Exports))
	return trimmed != "" && trimmed != "null"
}

func parsePackageJSON(fs FileSystem, path string) (*packageJSON, error) {
	data, err := fs.Read(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &pkg, nil
}

// resolveViaPackageJSON resolves an entry-point field to a concrete
// file under pkgRoot. ok is false when no field advertises the
// requested subpath.
func (r *Resolver) resolveViaPackageJSON(pkgRoot, subpath string, pkg *packageJSON) (string, bool) {
	if pkg.HasExports() {
		target, ok := resolveExportsField(pkg.Exports, subpath)
		if !ok {
			return "", false
		}
		candidate := filepath.Join(pkgRoot, filepath.FromSlash(target))
		if resolved, err := r.attemptResolve(candidate); err == nil {
			return resolved, true
		}
		return "", false
	}

	// main/types only cover the bare-package case; subpath imports
	// without an exports field fall through to the caller's walk.
	if subpath != "." {
		return "", false
	}
	for _, entry := range []string{pkg.Types, pkg.Typings, pkg.Main} {
		if entry == "" {
			continue
		}
		candidate := filepath.Join(pkgRoot, filepath.FromSlash(entry))
		if resolved, err := r.attemptResolve(candidate); err == nil {
			return resolved, true
		}
	}
	return "", false
}

// resolveExportsField picks a target string out of the `exports`
// field for the given subpath. Three shapes are handled:
//
//   - String:                "exports": "./dist/index.js"
//   - Conditional object:    {"types": "...", "import": "...", ...}
//   - Subpath-keyed object:  {".": "...", "./fp": "...", "./*": "./dist/*.js"}
//
// Pattern keys (single `*` wildcard) are matched after exact-match
// lookups; the most-specific (longest literal prefix) wins.
func resolveExportsField(raw json.RawMessage, subpath string) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	// String form is only valid for the bare-package subpath.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if subpath == "." {
			return asString, true
		}
		return "", false
	}

	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", false
	}

	// Subpath form is detected by any `.`-prefixed key — subpath
	// keys and condition keys can't legally coexist at the same level.
	for k := range asObject {
		if strings.HasPrefix(k, ".") {
			if val, ok := asObject[subpath]; ok {
				s := resolveExportsValue(val)
				return s, s != ""
			}
			// Try glob-pattern keys: `"./*"`, `"./fp/*"`, etc.
			s, ok := matchSubpathPattern(asObject, subpath)
			return s, ok
		}
	}

	// Conditional form is only valid for the bare-package subpath.
	if subpath != "." {
		return "", false
	}
	s := resolveExportsValue(raw)
	return s, s != ""
}

// matchSubpathPattern picks the best `*`-glob match for subpath out
// of a map of pattern keys to raw values. Used by both `exports`
// (`./*`) and `imports` (`#fp/*`) — the pattern shape is identical;
// only the prefix character differs and is irrelevant to matching.
// Most specific (longest literal prefix) wins.
func matchSubpathPattern(table map[string]json.RawMessage, subpath string) (string, bool) {
	type match struct {
		captured string
		value    string
	}
	var best match
	bestLen := -1
	for k, v := range table {
		star := strings.IndexByte(k, '*')
		if star < 0 {
			continue
		}
		prefix := k[:star]
		suffix := k[star+1:]
		if !strings.HasPrefix(subpath, prefix) || !strings.HasSuffix(subpath, suffix) {
			continue
		}
		// Don't treat the wildcard as matching zero chars when key
		// equals subpath without the `*` — exact-match handling
		// above takes that case.
		if len(subpath) < len(prefix)+len(suffix) {
			continue
		}
		captured := subpath[len(prefix) : len(subpath)-len(suffix)]
		raw := resolveExportsValue(v)
		if raw == "" {
			continue
		}
		if len(prefix) > bestLen {
			bestLen = len(prefix)
			best = match{captured: captured, value: raw}
		}
	}
	if bestLen < 0 {
		return "", false
	}
	return strings.Replace(best.value, "*", best.captured, 1), true
}

// resolveExportsValue picks a string out of a raw exports value
// (string or one-level conditional object). Priority order is
// types > import > default; first match wins.
func resolveExportsValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	for _, condition := range []string{"types", "import", "default"} {
		if v, ok := obj[condition]; ok {
			if s := resolveExportsValue(v); s != "" {
				return s
			}
		}
	}
	return ""
}
