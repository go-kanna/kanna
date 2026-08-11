package orm

import (
	"go/token"
	"go/types"
	"reflect"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
)

// Table is the plan for one //kanna:table struct.
type Table struct {
	Name      string // struct name, e.g. "User"
	TableName string // e.g. "users"; the directive's name= argument overrides inference
	Pos       token.Position
	Fields    []Field // column-backed fields in declaration order
	Relations []Relation
}

// PK returns the table's primary key field. Tables reports an error for a
// struct without one, so a Table that reaches the caller always has it.
func (t Table) PK() Field {
	for _, f := range t.Fields {
		if f.PrimaryKey {
			return f
		}
	}
	return Field{}
}

// Field is one column-backed struct field.
type Field struct {
	Name       string // Go field name
	Column     string
	GoType     string // rendered relative to the source package, e.g. "time.Time"
	PrimaryKey bool
	CreatedAt  bool
	UpdatedAt  bool
	Pos        token.Position
}

// Relation is one relation-tagged struct field, with everything the emitter
// needs already resolved: names are looked up, never reconstructed.
type Relation struct {
	FieldName     string
	Kind          string // has_many | has_one | belongs_to | many_to_many
	TargetType    string // bare target struct name, e.g. "Post"
	TargetPkgPath string // import path of the target's package; empty for the source package
	ForeignKey    string
	JoinTable     string // many_to_many only
	References    string // many_to_many only
	IsPointer     bool
	Pos           token.Position

	// Resolved by Tables once every table is known.
	ForeignKeyField string    // Go field owning the foreign key column
	FKIsPointer     bool      // belongs_to only: that field is a pointer
	KeyType         string    // preloader map key type
	TargetTableName string    // target's table name
	TargetPKColumn  string    // target's primary key column
	TargetPKField   string    // target's primary key Go field
	JoinScan        *JoinScan // belongs_to/has_one in the source package; nil otherwise

	named *types.Named // target type, for cross-package field lookups
}

// JoinScan carries what the emitter needs to scan a joined row directly into
// the relation field.
type JoinScan struct {
	Fields    []Field // the target's column fields
	PK        Field
	NullType  string // nullable scan wrapper for pointer relations, e.g. "sql.NullInt64"
	NullField string // its value accessor, e.g. ".Int64"
}

// Tables interprets the scanned structs: //kanna:table opts a struct in, orm
// tags describe its columns and relations, and everything malformed becomes a
// positioned diagnostic rather than a silent skip.
func Tables(structs []ir.Struct) ([]Table, []diag.Diag) {
	var (
		tables []Table
		diags  []diag.Diag
	)

	inPackage := make(map[string]bool, len(structs))
	for _, s := range structs {
		inPackage[s.Name] = true
	}

	marked := make(map[string]bool, len(structs))
	for _, s := range structs {
		d, msgs := directive.Find(s.Doc, "table")
		diags = append(diags, msgs.Diags(s.Pos)...)
		if !d.Found {
			diags = append(diags, warnUntaggedUse(s)...)
			continue
		}
		marked[s.Name] = true

		table, ds := buildTable(s, d.Args)
		diags = append(diags, ds...)
		if table != nil {
			tables = append(tables, *table)
		}
	}

	diags = append(diags, checkTableNames(tables)...)
	diags = append(diags, resolveRelations(tables, marked, inPackage)...)

	if diag.HasErrors(diags) {
		return nil, diags
	}
	return tables, diags
}

// warnUntaggedUse points out orm tags on a struct that never opted in, because
// the silent alternative is a model the author believes is handled.
func warnUntaggedUse(s ir.Struct) []diag.Diag {
	for _, f := range s.Fields {
		if _, ok := f.Tag.Lookup(tagKey); ok {
			return []diag.Diag{diag.Warningf(s.Pos,
				"%s has orm tags but no //kanna:table directive, so kanna-orm ignores it", s.Name)}
		}
	}
	return nil
}

func buildTable(s ir.Struct, args string) (*Table, []diag.Diag) {
	var diags []diag.Diag

	if s.Named.TypeParams().Len() > 0 {
		return nil, []diag.Diag{diag.Errorf(s.Pos, "%s is generic; kanna-orm cannot generate queries for it", s.Name)}
	}

	name, ds := parseTableArgs(args, s.Pos)
	diags = append(diags, ds...)
	if name == "" {
		name = tableName(s.Name)
	}

	table := Table{Name: s.Name, TableName: name, Pos: s.Pos}

	srcPkg := s.Named.Obj().Pkg()
	// The generated file lives in another package, so every named type is
	// qualified by its package name; basic types come out bare either way.
	qualify := func(p *types.Package) string { return p.Name() }

	for _, f := range s.Fields {
		if f.Embedded {
			diags = append(diags, diag.Warningf(f.Pos,
				"embedded field %s is ignored; declare its columns on %s directly", f.Name, s.Name))
			continue
		}
		if !f.Exported {
			continue
		}

		col, rel, errs := parseTag(f.Tag.Get(tagKey))
		for _, e := range errs {
			diags = append(diags, diag.Errorf(f.Pos, "%s.%s: %s", s.Name, f.Name, e))
		}
		if len(errs) > 0 {
			continue
		}

		if rel != nil {
			r, ds := buildRelation(s, f, rel, srcPkg)
			diags = append(diags, ds...)
			if r != nil {
				table.Relations = append(table.Relations, *r)
			}
			continue
		}

		field, ok := buildField(f, col, srcPkg, qualify)
		if ok {
			table.Fields = append(table.Fields, field)
		}
	}

	diags = append(diags, resolvePK(&table, s)...)
	diags = append(diags, checkTimestamps(table, s)...)
	diags = append(diags, checkColumns(table)...)

	return &table, diags
}

// parseTableArgs interprets the directive arguments: empty, or name=<table>.
func parseTableArgs(args string, pos token.Position) (string, []diag.Diag) {
	if args == "" {
		return "", nil
	}
	k, v, found := cutKeyValue(args)
	if !found || k != "name" || v == "" {
		return "", []diag.Diag{diag.Errorf(pos,
			"//kanna:table takes no argument or name=<table>, got %q", args)}
	}
	return v, nil
}

func cutKeyValue(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// buildField turns a column-tagged (or untagged) field into a Field. Untagged
// fields whose type reads as a relation — a slice of structs, or a pointer to
// a struct in the same package — are not columns and are skipped, the same way
// a hand-written query would not select them.
func buildField(f ir.Field, col *columnTag, srcPkg *types.Package, qualify types.Qualifier) (Field, bool) {
	if col.Skip {
		return Field{}, false
	}
	if col.Column == "" && !col.PrimaryKey && !col.CreatedAt && !col.UpdatedAt {
		if elem, isSlice := sliceElem(f.Type); isSlice {
			if elem, isPtr := pointerElem(elem); isPtr {
				if _, ok := structNamed(elem); ok {
					return Field{}, false
				}
			}
			if _, ok := structNamed(elem); ok {
				return Field{}, false
			}
		}
		if elem, isPtr := pointerElem(f.Type); isPtr {
			if named, ok := structNamed(elem); ok && named.Obj().Pkg() == srcPkg {
				return Field{}, false
			}
		}
	}

	column := col.Column
	if column == "" {
		column = camelToSnake(f.Name)
	}

	return Field{
		Name:       f.Name,
		Column:     column,
		GoType:     types.TypeString(f.Type, qualify),
		PrimaryKey: col.PrimaryKey,
		CreatedAt:  col.CreatedAt || f.Name == "CreatedAt",
		UpdatedAt:  col.UpdatedAt || f.Name == "UpdatedAt",
		Pos:        f.Pos,
	}, true
}

func buildRelation(s ir.Struct, f ir.Field, rel *relationTag, srcPkg *types.Package) (*Relation, []diag.Diag) {
	core := f.Type
	elem, isSlice := sliceElem(core)
	if isSlice {
		core = elem
	}
	elem, isPointer := pointerElem(core)
	if isPointer {
		core = elem
	}

	named, ok := structNamed(core)
	if !ok {
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: a %s field must be a struct, a pointer to one, or a slice of them", s.Name, f.Name, rel.Kind)}
	}

	wantSlice := rel.Kind == "has_many" || rel.Kind == "many_to_many"
	if wantSlice != isSlice {
		shape := "a slice"
		if !wantSlice {
			shape = "not a slice"
		}
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: a %s field must be %s", s.Name, f.Name, rel.Kind, shape)}
	}

	targetPkg := ""
	if p := named.Obj().Pkg(); p != nil && p != srcPkg {
		targetPkg = p.Path()
	}

	return &Relation{
		FieldName:     f.Name,
		Kind:          rel.Kind,
		TargetType:    named.Obj().Name(),
		TargetPkgPath: targetPkg,
		ForeignKey:    rel.ForeignKey,
		JoinTable:     rel.JoinTable,
		References:    rel.References,
		IsPointer:     isPointer,
		Pos:           f.Pos,
		named:         named,
	}, nil
}

// resolvePK settles the primary key: an explicit primary_key option wins, a
// field named ID is the fallback, anything else is an error. Two explicit
// keys are two mistakes with positions, not a silent pick.
func resolvePK(table *Table, s ir.Struct) []diag.Diag {
	var explicit []int
	for i, f := range table.Fields {
		if f.PrimaryKey {
			explicit = append(explicit, i)
		}
	}

	switch len(explicit) {
	case 1:
		return nil
	case 0:
		for i, f := range table.Fields {
			if f.Name == "ID" {
				table.Fields[i].PrimaryKey = true
				return nil
			}
		}
		return []diag.Diag{diag.Errorf(s.Pos,
			"%s has no primary key; name a field ID or tag one with orm:\",primary_key\"", s.Name)}
	default:
		diags := make([]diag.Diag, 0, len(explicit))
		for _, i := range explicit {
			diags = append(diags, diag.Errorf(table.Fields[i].Pos,
				"%s has more than one primary_key", s.Name))
		}
		return diags
	}
}

// checkTimestamps requires timestamp fields to be time.Time or *time.Time,
// because the generated setter calls IsZero or assigns a *time.Time and
// anything else fails to compile far from the mistake.
func checkTimestamps(table Table, s ir.Struct) []diag.Diag {
	var diags []diag.Diag
	for _, f := range table.Fields {
		if !f.CreatedAt && !f.UpdatedAt {
			continue
		}
		if f.GoType != "time.Time" && f.GoType != "*time.Time" {
			diags = append(diags, diag.Errorf(f.Pos,
				"%s.%s is a timestamp field but its type is %s, not time.Time", s.Name, f.Name, f.GoType))
		}
	}
	return diags
}

func checkColumns(table Table) []diag.Diag {
	var diags []diag.Diag
	seen := make(map[string]token.Position, len(table.Fields))
	for _, f := range table.Fields {
		if prev, ok := seen[f.Column]; ok {
			diags = append(diags, diag.Errorf(f.Pos,
				"column %q is already used at %s", f.Column, prev))
			continue
		}
		seen[f.Column] = f.Pos
	}
	return diags
}

func checkTableNames(tables []Table) []diag.Diag {
	var diags []diag.Diag
	seenTable := make(map[string]token.Position, len(tables))
	seenFactory := make(map[string]token.Position, len(tables))
	for _, t := range tables {
		if prev, ok := seenTable[t.TableName]; ok {
			diags = append(diags, diag.Errorf(t.Pos,
				"table %q is already generated for the struct at %s", t.TableName, prev))
			continue
		}
		seenTable[t.TableName] = t.Pos

		// Factories are named by pluralizing the struct, so "User" and a
		// struct literally named "Users" would collide in the output.
		factory := factoryName(t.Name)
		if prev, ok := seenFactory[factory]; ok {
			diags = append(diags, diag.Errorf(t.Pos,
				"generated factory %s collides with the one for the struct at %s", factory, prev))
			continue
		}
		seenFactory[factory] = t.Pos
	}
	return diags
}

// resolveRelations settles every relation once all tables are known: the
// target must generate queries itself (the preloader calls its factory), and
// every Go identifier the emitter will write is looked up here — from the
// plan for source-package targets, from type information for cross-package
// ones — never reconstructed from a column name.
func resolveRelations(tables []Table, marked, inPackage map[string]bool) []diag.Diag {
	byName := make(map[string]*Table, len(tables))
	for i := range tables {
		byName[tables[i].Name] = &tables[i]
	}

	var diags []diag.Diag
	for ti := range tables {
		t := &tables[ti]
		for ri := range t.Relations {
			r := &t.Relations[ri]
			if r.TargetPkgPath == "" {
				diags = append(diags, resolveLocal(t, r, byName, marked, inPackage)...)
			} else {
				diags = append(diags, resolveCrossPackage(t, r)...)
			}
		}
	}
	return diags
}

func resolveLocal(t *Table, r *Relation, byName map[string]*Table, marked, inPackage map[string]bool) []diag.Diag {
	if !marked[r.TargetType] {
		hint := "add //kanna:table to " + r.TargetType
		if !inPackage[r.TargetType] {
			hint = r.TargetType + " is not declared in this package"
		}
		return []diag.Diag{diag.Errorf(r.Pos,
			"%s.%s: relation target %s generates no queries", t.Name, r.FieldName, r.TargetType).
			WithHints(hint)}
	}

	target, ok := byName[r.TargetType]
	if !ok {
		return nil // the target itself failed to build; its own diagnostics explain why
	}

	r.TargetTableName = target.TableName
	r.TargetPKColumn = target.PK().Column
	r.TargetPKField = target.PK().Name

	switch r.Kind {
	case "belongs_to":
		fk := columnField(*t, r.ForeignKey)
		if fk == nil {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s", t.Name, r.FieldName, r.ForeignKey, t.Name)}
		}
		r.ForeignKeyField = fk.Name
		r.KeyType, r.FKIsPointer = derefType(fk.GoType)
	case "has_many", "has_one":
		fk := columnField(*target, r.ForeignKey)
		if fk == nil {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s", t.Name, r.FieldName, r.ForeignKey, r.TargetType)}
		}
		r.ForeignKeyField = fk.Name
		r.KeyType = t.PK().GoType
	default: // many_to_many
		r.KeyType = t.PK().GoType
	}

	if r.Kind == "belongs_to" || r.Kind == "has_one" {
		js := &JoinScan{Fields: target.Fields, PK: target.PK()}
		if r.IsPointer {
			js.NullType, js.NullField = nullTypeFor(js.PK.GoType)
		}
		r.JoinScan = js
	}
	return nil
}

// resolveCrossPackage fills what can be known about a target in another
// package. Its table name is inferred (a name= override over there is not
// visible from here) and its primary key column is taken to be "id" by the
// same convention ormgen used; the foreign key field is real, read from the
// target's type information rather than guessed from the column name.
func resolveCrossPackage(t *Table, r *Relation) []diag.Diag {
	r.TargetTableName = tableName(r.TargetType)
	r.TargetPKColumn = "id"
	r.TargetPKField = "ID"

	switch r.Kind {
	case "belongs_to":
		fk := columnField(*t, r.ForeignKey)
		if fk == nil {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s", t.Name, r.FieldName, r.ForeignKey, t.Name)}
		}
		r.ForeignKeyField = fk.Name
		r.KeyType, r.FKIsPointer = derefType(fk.GoType)
	case "has_many", "has_one":
		name, ok := fieldForColumn(r.named, r.ForeignKey)
		if !ok {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s.%s",
				t.Name, r.FieldName, r.ForeignKey, r.TargetPkgPath, r.TargetType)}
		}
		r.ForeignKeyField = name
		r.KeyType = t.PK().GoType
	default: // many_to_many
		r.KeyType = t.PK().GoType
	}
	return nil
}

// fieldForColumn finds the exported field of named whose column is column,
// reading orm tags the way the target's own generation run would.
func fieldForColumn(named *types.Named, column string) (string, bool) {
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return "", false
	}
	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() || f.Embedded() {
			continue
		}
		col, rel, errs := parseTag(reflect.StructTag(st.Tag(i)).Get(tagKey))
		if len(errs) > 0 || rel != nil || col.Skip {
			continue
		}
		name := col.Column
		if name == "" {
			name = camelToSnake(f.Name())
		}
		if name == column {
			return f.Name(), true
		}
	}
	return "", false
}

// derefType splits a possibly-pointer type string into its element and
// whether it was a pointer.
func derefType(goType string) (string, bool) {
	if len(goType) > 0 && goType[0] == '*' {
		return goType[1:], true
	}
	return goType, false
}

// nullTypeFor is the sql.Null wrapper a pointer relation scans its joined
// primary key through.
func nullTypeFor(goType string) (string, string) {
	if goType == "string" {
		return "sql.NullString", ".String"
	}
	return "sql.NullInt64", ".Int64"
}

func columnField(t Table, column string) *Field {
	for i, f := range t.Fields {
		if f.Column == column {
			return &t.Fields[i]
		}
	}
	return nil
}

func sliceElem(t types.Type) (types.Type, bool) {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return nil, false
	}
	return s.Elem(), true
}

func pointerElem(t types.Type) (types.Type, bool) {
	p, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return nil, false
	}
	return p.Elem(), true
}

func structNamed(t types.Type) (*types.Named, bool) {
	named, ok := t.(*types.Named)
	if !ok {
		return nil, false
	}
	_, ok = named.Underlying().(*types.Struct)
	return named, ok
}
