package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jairo-litman/ast-analyzer/extractor"
	"github.com/jairo-litman/ast-analyzer/graph"
)

// Load reconstructs a Project from the database. Files is nil —
// *sitter.Node pointers aren't part of the persistence contract.
func (s *Store) Load() (*graph.Project, error) {
	root, err := loadProjectRoot(s.db)
	if err != nil {
		return nil, err
	}
	files, err := loadFiles(s.db)
	if err != nil {
		return nil, err
	}

	p := &graph.Project{Root: root}

	if p.Symbols, err = loadSymbols(s.db, files); err != nil {
		return nil, err
	}
	if p.Calls, err = loadCalls(s.db, files); err != nil {
		return nil, err
	}
	if p.Imports, err = loadImports(s.db, files); err != nil {
		return nil, err
	}
	if p.ReExports, err = loadReExports(s.db, files); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadFileHashes returns the per-file content hashes keyed by
// project-relative path. Files saved without hashes return "".
func (s *Store) LoadFileHashes() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT path, content_hash FROM files`)
	if err != nil {
		return nil, fmt.Errorf("select file hashes: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("scan file hash: %w", err)
		}
		out[path] = hash
	}
	return out, rows.Err()
}

func loadProjectRoot(db *sql.DB) (string, error) {
	var root string
	err := db.QueryRow("SELECT value FROM project_meta WHERE key = 'root'").Scan(&root)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("select project root: %w", err)
	}
	return root, nil
}

func loadFiles(db *sql.DB) (map[int64]string, error) {
	rows, err := db.Query("SELECT id, path FROM files")
	if err != nil {
		return nil, fmt.Errorf("select files: %w", err)
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		out[id] = path
	}
	return out, rows.Err()
}

func loadSymbols(db *sql.DB, files map[int64]string) ([]graph.Symbol, error) {
	rows, err := db.Query(`SELECT id, kind, name, file_id, start_byte, end_byte, body_start_byte, details, is_default_export, type_refs FROM symbols`)
	if err != nil {
		return nil, fmt.Errorf("select symbols: %w", err)
	}
	defer rows.Close()

	var out []graph.Symbol
	for rows.Next() {
		var s graph.Symbol
		var fileID int64
		var kind, details, typeRefs string
		var isDefault int
		if err := rows.Scan(&s.ID, &kind, &s.Name, &fileID,
			&s.StartByte, &s.EndByte, &s.BodyStartByte, &details, &isDefault, &typeRefs); err != nil {
			return nil, fmt.Errorf("scan symbol: %w", err)
		}
		path, ok := files[fileID]
		if !ok {
			return nil, fmt.Errorf("symbol %q references missing file_id %d", s.ID, fileID)
		}
		s.Kind = graph.SymbolKind(kind)
		s.File = path
		s.IsDefaultExport = isDefault != 0
		if err := decodeSymbolDetails(&s, details); err != nil {
			return nil, fmt.Errorf("decode details for symbol %q: %w", s.ID, err)
		}
		if err := decodeTypeRefs(&s, typeRefs); err != nil {
			return nil, fmt.Errorf("decode type refs for symbol %q: %w", s.ID, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// decodeTypeRefs parses the symbols.type_refs JSON blob into s.
func decodeTypeRefs(s *graph.Symbol, blob string) error {
	if blob == "" {
		return nil
	}
	var refs []graph.SymbolTypeRef
	if err := json.Unmarshal([]byte(blob), &refs); err != nil {
		return err
	}
	s.TypeRefs = refs
	return nil
}

// loadReExports returns ReExports in INSERT order, which preserves
// the source order BuildProject emitted.
func loadReExports(db *sql.DB, files map[int64]string) ([]graph.ReExportEdge, error) {
	rows, err := db.Query(`SELECT id, file_id, path, resolved, kind, namespace, start_byte, end_byte FROM re_exports`)
	if err != nil {
		return nil, fmt.Errorf("select re_exports: %w", err)
	}
	defer rows.Close()

	var out []graph.ReExportEdge
	idToIndex := map[int64]int{}

	for rows.Next() {
		var id int64
		var e graph.ReExportEdge
		var fileID int64
		var kind string
		if err := rows.Scan(&id, &fileID, &e.Path, &e.Resolved, &kind, &e.Namespace,
			&e.StartByte, &e.EndByte); err != nil {
			return nil, fmt.Errorf("scan re_export: %w", err)
		}
		path, ok := files[fileID]
		if !ok {
			return nil, fmt.Errorf("re_export %d references missing file_id %d", id, fileID)
		}
		e.File = path
		e.Kind = extractor.ImportKind(kind)
		out = append(out, e)
		idToIndex[id] = len(out) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	bindRows, err := db.Query(`SELECT re_export_id, local_name, remote_name, is_type_only FROM re_export_bindings`)
	if err != nil {
		return nil, fmt.Errorf("select re_export_bindings: %w", err)
	}
	defer bindRows.Close()
	for bindRows.Next() {
		var reID int64
		var b graph.ReExportBinding
		var typeOnly int
		if err := bindRows.Scan(&reID, &b.LocalName, &b.RemoteName, &typeOnly); err != nil {
			return nil, fmt.Errorf("scan re_export_binding: %w", err)
		}
		idx, ok := idToIndex[reID]
		if !ok {
			return nil, fmt.Errorf("re_export_binding references unknown re_export_id %d", reID)
		}
		b.IsTypeOnly = typeOnly != 0
		out[idx].Bindings = append(out[idx].Bindings, b)
	}
	return out, bindRows.Err()
}

// decodeSymbolDetails parses the symbols.details JSON blob onto the
// kind-specific field of s. An empty blob leaves s untouched.
func decodeSymbolDetails(s *graph.Symbol, details string) error {
	if details == "" {
		return nil
	}
	switch s.Kind {
	case graph.SymbolClass:
		var cd graph.ClassDetails
		if err := json.Unmarshal([]byte(details), &cd); err != nil {
			return err
		}
		s.ClassDetails = &cd
	case graph.SymbolInterface:
		var id graph.InterfaceDetails
		if err := json.Unmarshal([]byte(details), &id); err != nil {
			return err
		}
		s.InterfaceDetails = &id
	case graph.SymbolFunction:
		var fd functionDetails
		if err := json.Unmarshal([]byte(details), &fd); err != nil {
			var lt map[string]string
			if legacyErr := json.Unmarshal([]byte(details), &lt); legacyErr != nil {
				return err
			}
			s.LocalTypes = lt
			return nil
		}
		s.LocalTypes = fd.LocalTypes
		s.LocalCallBindings = fd.LocalCallBindings
		s.LocalMethodBindings = fd.LocalMethodBindings
		s.LocalDestructureBindings = fd.LocalDestructureBindings
		s.InlineReturnProperties = fd.InlineReturnProperties
		s.LocalTypeOrigins = fd.LocalTypeOrigins
		s.ReturnType = fd.ReturnType
	}
	return nil
}

func loadCalls(db *sql.DB, files map[int64]string) ([]graph.CallSite, error) {
	rows, err := db.Query(`SELECT id, caller_id, callee, receiver, expression,
		is_constructor, file_id, start_byte, end_byte FROM calls`)
	if err != nil {
		return nil, fmt.Errorf("select calls: %w", err)
	}
	defer rows.Close()

	var out []graph.CallSite
	idToIndex := map[int64]int{}

	for rows.Next() {
		var id int64
		var c graph.CallSite
		var fileID int64
		var isCtor int
		if err := rows.Scan(&id, &c.CallerID, &c.Callee, &c.Receiver, &c.Expression,
			&isCtor, &fileID, &c.StartByte, &c.EndByte); err != nil {
			return nil, fmt.Errorf("scan call: %w", err)
		}
		path, ok := files[fileID]
		if !ok {
			return nil, fmt.Errorf("call %d references missing file_id %d", id, fileID)
		}
		c.IsConstructor = isCtor != 0
		c.File = path
		out = append(out, c)
		idToIndex[id] = len(out) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolutions come from a second query so the one-to-many join
	// doesn't blow up the calls row count.
	resRows, err := db.Query(`SELECT call_id, target_symbol_id FROM call_resolutions`)
	if err != nil {
		return nil, fmt.Errorf("select call_resolutions: %w", err)
	}
	defer resRows.Close()
	for resRows.Next() {
		var callID int64
		var target string
		if err := resRows.Scan(&callID, &target); err != nil {
			return nil, fmt.Errorf("scan call_resolution: %w", err)
		}
		idx, ok := idToIndex[callID]
		if !ok {
			return nil, fmt.Errorf("call_resolution references unknown call_id %d", callID)
		}
		out[idx].ResolvedTo = append(out[idx].ResolvedTo, target)
	}
	return out, resRows.Err()
}

func loadImports(db *sql.DB, files map[int64]string) ([]graph.ImportEdge, error) {
	rows, err := db.Query(`SELECT id, file_id, path, resolved, kind, namespace, start_byte, end_byte FROM imports`)
	if err != nil {
		return nil, fmt.Errorf("select imports: %w", err)
	}
	defer rows.Close()

	var out []graph.ImportEdge
	idToIndex := map[int64]int{}

	for rows.Next() {
		var id int64
		var e graph.ImportEdge
		var fileID int64
		var kind string
		if err := rows.Scan(&id, &fileID, &e.Path, &e.Resolved, &kind, &e.Namespace,
			&e.StartByte, &e.EndByte); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		path, ok := files[fileID]
		if !ok {
			return nil, fmt.Errorf("import %d references missing file_id %d", id, fileID)
		}
		e.File = path
		e.Kind = extractor.ImportKind(kind)
		out = append(out, e)
		idToIndex[id] = len(out) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	idRows, err := db.Query(`SELECT import_id, local_name, remote_name, is_type_only FROM import_identifiers`)
	if err != nil {
		return nil, fmt.Errorf("select import_identifiers: %w", err)
	}
	defer idRows.Close()
	for idRows.Next() {
		var importID int64
		var ident extractor.IdentifierContext
		var typeOnly int
		if err := idRows.Scan(&importID, &ident.LocalName, &ident.RemoteName, &typeOnly); err != nil {
			return nil, fmt.Errorf("scan import_identifier: %w", err)
		}
		idx, ok := idToIndex[importID]
		if !ok {
			return nil, fmt.Errorf("import_identifier references unknown import_id %d", importID)
		}
		ident.IsTypeOnly = typeOnly != 0
		out[idx].Identifiers = append(out[idx].Identifiers, ident)
	}
	return out, idRows.Err()
}
