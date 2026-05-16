package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jairo-litman/ast-analyzer/graph"
)

// Save replaces the database contents with the given Project, leaving
// per-file content hashes empty. Files saved this way will be treated
// as "changed" on the next incremental index.
func (s *Store) Save(p *graph.Project) error {
	return s.SaveWithHashes(p, nil)
}

// SaveWithHashes is Save plus per-file content hashes. Keys are
// project-relative paths; files referenced by p but absent from the
// map get an empty hash row.
//
// All existing rows are dropped (cascading from `files`) and re-
// inserted in a single transaction, so a partial save can't corrupt
// the on-disk state.
func (s *Store) SaveWithHashes(p *graph.Project, hashes map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin save: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec("DELETE FROM files;"); err != nil {
		return fmt.Errorf("clear files: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM project_meta;"); err != nil {
		return fmt.Errorf("clear project_meta: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO project_meta(key, value) VALUES ('root', ?)", p.Root); err != nil {
		return fmt.Errorf("save project root: %w", err)
	}

	fileIDs, err := insertFiles(tx, p, hashes)
	if err != nil {
		return err
	}
	if err := insertSymbols(tx, p, fileIDs); err != nil {
		return err
	}
	if err := insertCalls(tx, p, fileIDs); err != nil {
		return err
	}
	if err := insertImports(tx, p, fileIDs); err != nil {
		return err
	}
	if err := insertReExports(tx, p, fileIDs); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save: %w", err)
	}
	committed = true
	return nil
}

// insertReExports persists every re-export edge plus its bindings.
func insertReExports(tx *sql.Tx, p *graph.Project, fileIDs map[string]int64) error {
	reStmt, err := tx.Prepare(`INSERT INTO re_exports
		(file_id, path, resolved, kind, namespace, start_byte, end_byte)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare re_exports insert: %w", err)
	}
	defer reStmt.Close()

	bindStmt, err := tx.Prepare(`INSERT INTO re_export_bindings
		(re_export_id, local_name, remote_name, is_type_only)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare re_export_bindings insert: %w", err)
	}
	defer bindStmt.Close()

	for _, re := range p.ReExports {
		fid, ok := fileIDs[re.File]
		if !ok {
			return fmt.Errorf("re-export %q references unknown file %q", re.Path, re.File)
		}
		res, err := reStmt.Exec(fid, re.Path, re.Resolved, string(re.Kind),
			re.Namespace, re.StartByte, re.EndByte)
		if err != nil {
			return fmt.Errorf("insert re-export %q in %q: %w", re.Path, re.File, err)
		}
		reID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("re-export id for %q in %q: %w", re.Path, re.File, err)
		}
		for _, b := range re.Bindings {
			if _, err := bindStmt.Exec(reID, b.LocalName, b.RemoteName, boolToInt(b.IsTypeOnly)); err != nil {
				return fmt.Errorf("insert re_export_binding %q for %d: %w", b.LocalName, reID, err)
			}
		}
	}
	return nil
}

// insertFiles populates the `files` table with every distinct path
// referenced by p or present in hashes (so files with no declarations
// still record their hash). Returns a path → file_id map.
func insertFiles(tx *sql.Tx, p *graph.Project, hashes map[string]string) (map[string]int64, error) {
	paths := map[string]struct{}{}
	for _, s := range p.Symbols {
		paths[s.File] = struct{}{}
	}
	for _, c := range p.Calls {
		paths[c.File] = struct{}{}
	}
	for _, imp := range p.Imports {
		paths[imp.File] = struct{}{}
	}
	for _, re := range p.ReExports {
		paths[re.File] = struct{}{}
	}
	for path := range hashes {
		paths[path] = struct{}{}
	}

	stmt, err := tx.Prepare("INSERT INTO files(path, content_hash) VALUES (?, ?)")
	if err != nil {
		return nil, fmt.Errorf("prepare files insert: %w", err)
	}
	defer stmt.Close()

	out := make(map[string]int64, len(paths))
	for path := range paths {
		res, err := stmt.Exec(path, hashes[path])
		if err != nil {
			return nil, fmt.Errorf("insert file %q: %w", path, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("file id for %q: %w", path, err)
		}
		out[path] = id
	}
	return out, nil
}

func insertSymbols(tx *sql.Tx, p *graph.Project, fileIDs map[string]int64) error {
	stmt, err := tx.Prepare(`INSERT INTO symbols
		(id, kind, name, file_id, start_byte, end_byte, body_start_byte, details, is_default_export, type_refs)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare symbols insert: %w", err)
	}
	defer stmt.Close()

	for _, s := range p.Symbols {
		fid, ok := fileIDs[s.File]
		if !ok {
			return fmt.Errorf("symbol %q references unknown file %q", s.ID, s.File)
		}
		details, err := encodeSymbolDetails(s)
		if err != nil {
			return fmt.Errorf("encode details for symbol %q: %w", s.ID, err)
		}
		typeRefs, err := encodeTypeRefs(s.TypeRefs)
		if err != nil {
			return fmt.Errorf("encode type refs for symbol %q: %w", s.ID, err)
		}
		if _, err := stmt.Exec(s.ID, string(s.Kind), s.Name, fid,
			s.StartByte, s.EndByte, s.BodyStartByte, details,
			boolToInt(s.IsDefaultExport), typeRefs); err != nil {
			return fmt.Errorf("insert symbol %q: %w", s.ID, err)
		}
	}
	return nil
}

// encodeTypeRefs serialises refs as JSON for symbols.type_refs.
// Empty slice maps to the empty string.
func encodeTypeRefs(refs []graph.SymbolTypeRef) (string, error) {
	if len(refs) == 0 {
		return "", nil
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// encodeSymbolDetails serialises kind-specific metadata for the
// symbols.details column. Returns "" for kinds that carry no extras.
func encodeSymbolDetails(s graph.Symbol) (string, error) {
	switch s.Kind {
	case graph.SymbolClass:
		if s.ClassDetails == nil {
			return "", nil
		}
		b, err := json.Marshal(s.ClassDetails)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case graph.SymbolInterface:
		if s.InterfaceDetails == nil {
			return "", nil
		}
		b, err := json.Marshal(s.InterfaceDetails)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case graph.SymbolFunction:
		if len(s.LocalTypes) == 0 && len(s.LocalCallBindings) == 0 && len(s.LocalMethodBindings) == 0 && len(s.LocalDestructureBindings) == 0 && len(s.InlineReturnProperties) == 0 && len(s.LocalTypeOrigins) == 0 && s.ReturnType == "" {
			return "", nil
		}
		b, err := json.Marshal(functionDetails{
			LocalTypes:               s.LocalTypes,
			LocalCallBindings:        s.LocalCallBindings,
			LocalMethodBindings:      s.LocalMethodBindings,
			LocalDestructureBindings: s.LocalDestructureBindings,
			InlineReturnProperties:   s.InlineReturnProperties,
			LocalTypeOrigins:         s.LocalTypeOrigins,
			ReturnType:               s.ReturnType,
		})
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", nil
}

// functionDetails is the JSON envelope persisted in symbols.details
// for function-kind symbols. Older indexes wrote just the
// LocalTypes map at the top level; Load handles that legacy shape
// as a fallback so re-indexing isn't strictly required.
type functionDetails struct {
	LocalTypes               map[string]string                       `json:"local_types,omitempty"`
	LocalCallBindings        map[string]string                       `json:"local_call_bindings,omitempty"`
	LocalMethodBindings      map[string]graph.LocalMethodTarget      `json:"local_method_bindings,omitempty"`
	LocalDestructureBindings map[string]graph.LocalDestructureSource `json:"local_destructure_bindings,omitempty"`
	InlineReturnProperties   map[string]graph.InlineReturnSource     `json:"inline_return_properties,omitempty"`
	LocalTypeOrigins         map[string]graph.TypeOrigin             `json:"local_type_origins,omitempty"`
	ReturnType               string                                  `json:"return_type,omitempty"`
}

func insertCalls(tx *sql.Tx, p *graph.Project, fileIDs map[string]int64) error {
	callStmt, err := tx.Prepare(`INSERT INTO calls
		(caller_id, callee, receiver, expression, is_constructor, file_id, start_byte, end_byte)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare calls insert: %w", err)
	}
	defer callStmt.Close()

	resStmt, err := tx.Prepare(`INSERT INTO call_resolutions
		(call_id, target_symbol_id) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare call_resolutions insert: %w", err)
	}
	defer resStmt.Close()

	for _, c := range p.Calls {
		fid, ok := fileIDs[c.File]
		if !ok {
			return fmt.Errorf("call from %q references unknown file %q", c.CallerID, c.File)
		}
		res, err := callStmt.Exec(
			c.CallerID, c.Callee, c.Receiver, c.Expression,
			boolToInt(c.IsConstructor), fid, c.StartByte, c.EndByte,
		)
		if err != nil {
			return fmt.Errorf("insert call from %q: %w", c.CallerID, err)
		}
		callID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("call id for %q: %w", c.CallerID, err)
		}
		for _, target := range c.ResolvedTo {
			if _, err := resStmt.Exec(callID, target); err != nil {
				return fmt.Errorf("insert call_resolution %d -> %q: %w", callID, target, err)
			}
		}
	}
	return nil
}

func insertImports(tx *sql.Tx, p *graph.Project, fileIDs map[string]int64) error {
	impStmt, err := tx.Prepare(`INSERT INTO imports
		(file_id, path, resolved, kind, namespace, start_byte, end_byte)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare imports insert: %w", err)
	}
	defer impStmt.Close()

	idStmt, err := tx.Prepare(`INSERT INTO import_identifiers
		(import_id, local_name, remote_name, is_type_only)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare import_identifiers insert: %w", err)
	}
	defer idStmt.Close()

	for _, imp := range p.Imports {
		fid, ok := fileIDs[imp.File]
		if !ok {
			return fmt.Errorf("import in %q references unknown file %q", imp.Path, imp.File)
		}
		res, err := impStmt.Exec(fid, imp.Path, imp.Resolved, string(imp.Kind), imp.Namespace,
			imp.StartByte, imp.EndByte)
		if err != nil {
			return fmt.Errorf("insert import %q in %q: %w", imp.Path, imp.File, err)
		}
		impID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("import id for %q in %q: %w", imp.Path, imp.File, err)
		}
		for _, id := range imp.Identifiers {
			if _, err := idStmt.Exec(impID, id.LocalName, id.RemoteName, boolToInt(id.IsTypeOnly)); err != nil {
				return fmt.Errorf("insert identifier %q for import %d: %w", id.LocalName, impID, err)
			}
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
