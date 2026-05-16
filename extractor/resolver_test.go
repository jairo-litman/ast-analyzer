package extractor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFS is an explicit-membership stand-in for OSFileSystem.
// contents lets a test seed package.json bytes (or any other file
// the resolver needs to Read).
type mockFS struct {
	files    map[string]bool
	dirs     map[string]bool
	contents map[string][]byte
}

func newMockFS(files []string, dirs []string) *mockFS {
	m := &mockFS{
		files:    make(map[string]bool, len(files)),
		dirs:     make(map[string]bool, len(dirs)),
		contents: map[string][]byte{},
	}
	for _, f := range files {
		m.files[f] = true
	}
	for _, d := range dirs {
		m.dirs[d] = true
	}
	return m
}

func (m *mockFS) Stat(path string) (isFile, isDir bool) {
	return m.files[path], m.dirs[path]
}

func (m *mockFS) Read(path string) ([]byte, error) {
	if data, ok := m.contents[path]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("mockFS: not found: %s", path)
}

func TestParseConfig(t *testing.T) {
	config, err := parseConfigChain(OSFileSystem{}, "testdata/resolver/tsconfig.json")
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "testdata/resolver", config.BaseDir)
	assert.Equal(t, map[string][]string{
		"@/*":   {"src/*"},
		"lib/*": {"common/lib/*", "vendor/*"},
	}, config.Paths)
}

func TestResolvePath(t *testing.T) {
	config := &ResolverConfig{
		BaseDir: "/project",
		Paths: map[string][]string{
			"@/*":   {"src/*"},
			"lib/*": {"common/lib/*", "vendor/*"},
		},
	}

	files := []string{
		"/project/src/main.ts",
		"/project/src/math.ts",
		"/project/src/utils/math.ts",
		"/project/src/components/Button.ts",
		"/project/src/common/lib/helper.ts",
		"/project/vendor/helper.ts",
		"/project/node_modules/lodash/index.d.ts",
		"/project/src/widgets/index.ts",
	}
	dirs := []string{
		"/project/src/widgets",
		"/project/node_modules/lodash",
	}

	resolver := NewResolver(config, newMockFS(files, dirs))

	cases := []struct {
		name       string
		importPath string
		sourceFile string
		want       string
		wantErr    bool
	}{
		{
			name:       "relative import in same directory",
			importPath: "./math",
			sourceFile: "/project/src/main.ts",
			want:       "/project/src/math.ts",
		},
		{
			name:       "relative import via parent directory",
			importPath: "../common/lib/helper",
			sourceFile: "/project/src/utils/math.ts",
			want:       "/project/src/common/lib/helper.ts",
		},
		{
			name:       "relative import via grandparent directory",
			importPath: "../../src/components/Button",
			sourceFile: "/project/src/utils/math.ts",
			want:       "/project/src/components/Button.ts",
		},
		{
			name:       "alias resolves first matching target",
			importPath: "@/components/Button",
			sourceFile: "/project/src/main.ts",
			want:       "/project/src/components/Button.ts",
		},
		{
			name:       "alias falls through to second target when first misses",
			importPath: "lib/helper",
			sourceFile: "/project/src/main.ts",
			want:       "/project/vendor/helper.ts",
		},
		{
			name:       "node_modules walk finds package via index.d.ts",
			importPath: "lodash",
			sourceFile: "/project/src/main.ts",
			want:       "/project/node_modules/lodash/index.d.ts",
		},
		{
			name:       "directory import resolves via index file",
			importPath: "./widgets",
			sourceFile: "/project/src/main.ts",
			want:       "/project/src/widgets/index.ts",
		},
		{
			name:       "missing relative import errors out",
			importPath: "./ghost",
			sourceFile: "/project/src/main.ts",
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolver.Resolve(tc.importPath, tc.sourceFile)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveNodeModuleViaPackageJSON drives the package.json
// dispatch through the Resolver under the mock FS. Each case declares
// the package dir, manifest contents, and the entry file the manifest
// points at.
func TestResolveNodeModuleViaPackageJSON(t *testing.T) {
	const sourceFile = "/proj/src/main.ts"

	cases := []struct {
		name    string
		pkgRoot string
		pkgJSON string
		// files maps relative-to-pkgRoot path → membership marker.
		entryFile  string
		importPath string
		want       string
		wantErr    bool
	}{
		{
			name:       "types field resolves",
			pkgRoot:    "/proj/node_modules/lodash",
			pkgJSON:    `{"types": "./dist/index.d.ts"}`,
			entryFile:  "/proj/node_modules/lodash/dist/index.d.ts",
			importPath: "lodash",
			want:       "/proj/node_modules/lodash/dist/index.d.ts",
		},
		{
			name:       "typings as alias for types",
			pkgRoot:    "/proj/node_modules/legacy",
			pkgJSON:    `{"typings": "./dist/index.d.ts"}`,
			entryFile:  "/proj/node_modules/legacy/dist/index.d.ts",
			importPath: "legacy",
			want:       "/proj/node_modules/legacy/dist/index.d.ts",
		},
		{
			name:       "main field resolves when no types",
			pkgRoot:    "/proj/node_modules/runtime-only",
			pkgJSON:    `{"main": "./lib/index.js"}`,
			entryFile:  "/proj/node_modules/runtime-only/lib/index.js",
			importPath: "runtime-only",
			want:       "/proj/node_modules/runtime-only/lib/index.js",
		},
		{
			name:       "types preferred over main when both present",
			pkgRoot:    "/proj/node_modules/both",
			pkgJSON:    `{"main": "./lib/index.js", "types": "./types/index.d.ts"}`,
			entryFile:  "/proj/node_modules/both/types/index.d.ts",
			importPath: "both",
			want:       "/proj/node_modules/both/types/index.d.ts",
		},
		{
			name:       "exports as bare string for the bare package",
			pkgRoot:    "/proj/node_modules/strpkg",
			pkgJSON:    `{"exports": "./dist/index.js"}`,
			entryFile:  "/proj/node_modules/strpkg/dist/index.js",
			importPath: "strpkg",
			want:       "/proj/node_modules/strpkg/dist/index.js",
		},
		{
			name:       "exports conditional object picks types first",
			pkgRoot:    "/proj/node_modules/cond",
			pkgJSON:    `{"exports": {"types": "./dist/index.d.ts", "import": "./dist/index.mjs"}}`,
			entryFile:  "/proj/node_modules/cond/dist/index.d.ts",
			importPath: "cond",
			want:       "/proj/node_modules/cond/dist/index.d.ts",
		},
		{
			name:       "exports subpath object resolves the requested path",
			pkgRoot:    "/proj/node_modules/sub",
			pkgJSON:    `{"exports": {".": "./index.js", "./fp": "./fp/index.js"}}`,
			entryFile:  "/proj/node_modules/sub/fp/index.js",
			importPath: "sub/fp",
			want:       "/proj/node_modules/sub/fp/index.js",
		},
		{
			name:       "exports strict mode: subpath not advertised → fail",
			pkgRoot:    "/proj/node_modules/strict",
			pkgJSON:    `{"exports": {".": "./index.js"}}`,
			entryFile:  "/proj/node_modules/strict/internal.js", // exists but not advertised
			importPath: "strict/internal",
			wantErr:    true,
		},
		{
			name:       "scoped package types field",
			pkgRoot:    "/proj/node_modules/@types/node",
			pkgJSON:    `{"types": "./index.d.ts"}`,
			entryFile:  "/proj/node_modules/@types/node/index.d.ts",
			importPath: "@types/node",
			want:       "/proj/node_modules/@types/node/index.d.ts",
		},
		{
			name:       "no package.json → falls through to colocated index walk",
			pkgRoot:    "/proj/node_modules/legacy-pkg",
			pkgJSON:    "", // absent
			entryFile:  "/proj/node_modules/legacy-pkg/index.d.ts",
			importPath: "legacy-pkg",
			want:       "/proj/node_modules/legacy-pkg/index.d.ts",
		},
		{
			name:       "malformed package.json → falls through to colocated index walk",
			pkgRoot:    "/proj/node_modules/broken",
			pkgJSON:    `{"types": `, // truncated
			entryFile:  "/proj/node_modules/broken/index.d.ts",
			importPath: "broken",
			want:       "/proj/node_modules/broken/index.d.ts",
		},
		{
			name:       "exports subpath glob resolves with `*` substitution",
			pkgRoot:    "/proj/node_modules/glob",
			pkgJSON:    `{"exports": {".": "./index.js", "./*": "./dist/*.js"}}`,
			entryFile:  "/proj/node_modules/glob/dist/foo.js",
			importPath: "glob/foo",
			want:       "/proj/node_modules/glob/dist/foo.js",
		},
		{
			name:       "exports subpath glob with literal prefix wins over `./*`",
			pkgRoot:    "/proj/node_modules/glob2",
			pkgJSON:    `{"exports": {"./fp/*": "./dist/fp/*.js", "./*": "./dist/*.js"}}`,
			entryFile:  "/proj/node_modules/glob2/dist/fp/curry.js",
			importPath: "glob2/fp/curry",
			want:       "/proj/node_modules/glob2/dist/fp/curry.js",
		},
		{
			name:       "exports subpath glob: unmatched subpath fails (strict)",
			pkgRoot:    "/proj/node_modules/strictglob",
			pkgJSON:    `{"exports": {"./fp/*": "./dist/fp/*.js"}}`,
			entryFile:  "/proj/node_modules/strictglob/lib/other.js",
			importPath: "strictglob/lib/other",
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []string{tc.entryFile}
			dirs := []string{tc.pkgRoot, "/proj/src"}
			if tc.pkgJSON != "" {
				files = append(files, tc.pkgRoot+"/package.json")
			}

			fs := newMockFS(files, dirs)
			if tc.pkgJSON != "" {
				fs.contents[tc.pkgRoot+"/package.json"] = []byte(tc.pkgJSON)
			}

			resolver := NewResolver(&ResolverConfig{}, fs)

			got, err := resolver.Resolve(tc.importPath, sourceFile)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveSubpathImport pins `#name` imports against a
// package.json's `imports` field. The resolver walks up from the
// source file looking for the nearest package.json with a matching
// entry.
func TestResolveSubpathImport(t *testing.T) {
	cases := []struct {
		name       string
		pkgRoot    string
		pkgJSON    string
		entryFile  string
		sourceFile string
		importPath string
		want       string
		wantErr    bool
	}{
		{
			name:       "exact #name match",
			pkgRoot:    "/proj",
			pkgJSON:    `{"imports": {"#util": "./src/util.ts"}}`,
			entryFile:  "/proj/src/util.ts",
			sourceFile: "/proj/src/main.ts",
			importPath: "#util",
			want:       "/proj/src/util.ts",
		},
		{
			name:       "subpath glob substitutes the captured portion",
			pkgRoot:    "/proj",
			pkgJSON:    `{"imports": {"#fp/*": "./src/fp/*.ts"}}`,
			entryFile:  "/proj/src/fp/curry.ts",
			sourceFile: "/proj/src/main.ts",
			importPath: "#fp/curry",
			want:       "/proj/src/fp/curry.ts",
		},
		{
			name:       "exact match wins over glob",
			pkgRoot:    "/proj",
			pkgJSON:    `{"imports": {"#fp/curry": "./src/special.ts", "#fp/*": "./src/fp/*.ts"}}`,
			entryFile:  "/proj/src/special.ts",
			sourceFile: "/proj/src/main.ts",
			importPath: "#fp/curry",
			want:       "/proj/src/special.ts",
		},
		{
			name:       "conditional value picks types > import > default",
			pkgRoot:    "/proj",
			pkgJSON:    `{"imports": {"#util": {"types": "./types/util.d.ts", "import": "./src/util.js"}}}`,
			entryFile:  "/proj/types/util.d.ts",
			sourceFile: "/proj/src/main.ts",
			importPath: "#util",
			want:       "/proj/types/util.d.ts",
		},
		{
			name:       "no matching imports entry → error",
			pkgRoot:    "/proj",
			pkgJSON:    `{"imports": {"#util": "./src/util.ts"}}`,
			entryFile:  "/proj/src/util.ts",
			sourceFile: "/proj/src/main.ts",
			importPath: "#missing",
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []string{
				tc.pkgRoot + "/package.json",
				tc.entryFile,
			}
			dirs := []string{tc.pkgRoot, "/proj/src"}
			fs := newMockFS(files, dirs)
			fs.contents[tc.pkgRoot+"/package.json"] = []byte(tc.pkgJSON)

			resolver := NewResolver(&ResolverConfig{}, fs)
			got, err := resolver.Resolve(tc.importPath, tc.sourceFile)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseConfigChain drives parseConfigChain through the mock FS,
// declaring each tsconfig hierarchy as inline JSON.
func TestParseConfigChain(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string // absolute path → tsconfig contents
		entry     string
		wantBase  string
		wantPaths map[string][]string
	}{
		{
			name: "single extends with .json extension",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./base.json", "compilerOptions": {"paths": {"@local/*": ["src/*"]}}}`,
				"/proj/base.json":     `{"compilerOptions": {"paths": {"@base/*": ["common/*"]}}}`,
			},
			entry:    "/proj/tsconfig.json",
			wantBase: "/proj",
			wantPaths: map[string][]string{
				"@local/*": {"src/*"},
				"@base/*":  {"common/*"},
			},
		},
		{
			name: "extends without .json extension is appended",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./base"}`,
				"/proj/base.json":     `{"compilerOptions": {"paths": {"@b/*": ["b/*"]}}}`,
			},
			entry:     "/proj/tsconfig.json",
			wantBase:  "/proj",
			wantPaths: map[string][]string{"@b/*": {"b/*"}},
		},
		{
			name: "two-level chain inherits all paths",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./mid.json"}`,
				"/proj/mid.json":      `{"extends": "./grand.json", "compilerOptions": {"paths": {"@mid/*": ["mid/*"]}}}`,
				"/proj/grand.json":    `{"compilerOptions": {"paths": {"@grand/*": ["grand/*"]}}}`,
			},
			entry:    "/proj/tsconfig.json",
			wantBase: "/proj",
			wantPaths: map[string][]string{
				"@mid/*":   {"mid/*"},
				"@grand/*": {"grand/*"},
			},
		},
		{
			name: "child overrides parent on key collision",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./base.json", "compilerOptions": {"paths": {"@a/*": ["child/*"]}}}`,
				"/proj/base.json":     `{"compilerOptions": {"paths": {"@a/*": ["parent/*"], "@b/*": ["base/*"]}}}`,
			},
			entry:    "/proj/tsconfig.json",
			wantBase: "/proj",
			wantPaths: map[string][]string{
				"@a/*": {"child/*"},
				"@b/*": {"base/*"},
			},
		},
		{
			name: "child inherits parent's resolved baseUrl when unset",
			files: map[string]string{
				"/proj/sub/tsconfig.json": `{"extends": "../base.json"}`,
				"/proj/base.json":         `{"compilerOptions": {"baseUrl": "./common"}}`,
			},
			entry:     "/proj/sub/tsconfig.json",
			wantBase:  "/proj/common",
			wantPaths: map[string][]string{},
		},
		{
			name: "child overrides parent's baseUrl when set",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./base.json", "compilerOptions": {"baseUrl": "./mine"}}`,
				"/proj/base.json":     `{"compilerOptions": {"baseUrl": "./theirs"}}`,
			},
			entry:     "/proj/tsconfig.json",
			wantBase:  "/proj/mine",
			wantPaths: map[string][]string{},
		},
		{
			name: "cycle terminates gracefully",
			files: map[string]string{
				"/proj/a.json": `{"extends": "./b.json", "compilerOptions": {"paths": {"@a/*": ["a/*"]}}}`,
				"/proj/b.json": `{"extends": "./a.json", "compilerOptions": {"paths": {"@b/*": ["b/*"]}}}`,
			},
			entry:    "/proj/a.json",
			wantBase: "/proj",
			wantPaths: map[string][]string{
				"@a/*": {"a/*"},
				"@b/*": {"b/*"},
			},
		},
		{
			name: "missing parent silently degrades to child's own data",
			files: map[string]string{
				"/proj/tsconfig.json": `{"extends": "./missing.json", "compilerOptions": {"paths": {"@x/*": ["src/*"]}}}`,
			},
			entry:     "/proj/tsconfig.json",
			wantBase:  "/proj",
			wantPaths: map[string][]string{"@x/*": {"src/*"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			for p := range tc.files {
				paths = append(paths, p)
			}
			fs := newMockFS(paths, nil)
			for p, contents := range tc.files {
				fs.contents[p] = []byte(contents)
			}

			got, err := parseConfigChain(fs, tc.entry)
			require.NoError(t, err)
			assert.Equal(t, tc.wantBase, got.BaseDir, "BaseDir")
			assert.Equal(t, tc.wantPaths, got.Paths, "Paths")
		})
	}
}

func TestResolveExportsField_internals(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		subpath string
		want    string
		wantOK  bool
	}{
		{name: "string for bare package", raw: `"./index.js"`, subpath: ".", want: "./index.js", wantOK: true},
		{name: "string ignored for subpath", raw: `"./index.js"`, subpath: "./fp", wantOK: false},
		{name: "conditional types wins", raw: `{"types": "./t.d.ts", "import": "./i.mjs"}`, subpath: ".", want: "./t.d.ts", wantOK: true},
		{name: "conditional import when no types", raw: `{"import": "./i.mjs", "default": "./d.js"}`, subpath: ".", want: "./i.mjs", wantOK: true},
		{name: "conditional default fallback", raw: `{"default": "./d.js"}`, subpath: ".", want: "./d.js", wantOK: true},
		{name: "subpath object exact match", raw: `{".": "./i.js", "./fp": "./fp.js"}`, subpath: "./fp", want: "./fp.js", wantOK: true},
		{name: "subpath unknown is strict miss", raw: `{".": "./i.js"}`, subpath: "./missing", wantOK: false},
		{name: "nested conditional inside subpath", raw: `{"./fp": {"types": "./fp.d.ts"}}`, subpath: "./fp", want: "./fp.d.ts", wantOK: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveExportsField([]byte(tc.raw), tc.subpath)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
