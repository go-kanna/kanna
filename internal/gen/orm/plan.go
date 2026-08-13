package orm

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/relation"
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

// PkgRef names a package a rendered type string refers to, so the emitter
// can import what the string mentions.
type PkgRef struct {
	Name string
	Path string
}

// Field is one column-backed struct field.
type Field struct {
	Name       string // Go field name
	Column     string
	GoType     string   // rendered relative to the source package, e.g. "time.Time"
	IntKind    bool     // underlying type is an integer, however it is named
	Comparable bool     // usable as a map key
	TypePkgs   []PkgRef // packages GoType mentions
	PrimaryKey bool
	CreatedAt  bool
	UpdatedAt  bool
	Pos        token.Position
}

// Relation is one relation-tagged struct field, with everything the emitter
// needs already resolved: names are looked up, never reconstructed.
type Relation struct {
	FieldName  string
	Kind       string // has_many | has_one | belongs_to | many_to_many
	TargetType string // bare target struct name, always in the source package
	ForeignKey string
	JoinTable  string // many_to_many only
	References string // many_to_many only
	IsPointer  bool
	Pos        token.Position

	// Resolved by Tables once every table is known.
	ForeignKeyField string    // Go field owning the foreign key column
	FKIsPointer     bool      // the foreign key field is a pointer
	KeyType         string    // preloader map key type on the parent side
	TargetKeyType   string    // the target's primary key type
	TypeDeps        []PkgRef  // packages the key type strings mention
	TargetTableName string    // target's table name
	TargetPKColumn  string    // target's primary key column
	TargetPKField   string    // target's primary key Go field
	JoinScan        *JoinScan // belongs_to/has_one only; nil otherwise
}

// JoinScan carries what the emitter needs to scan a joined row directly into
// the relation field.
type JoinScan struct {
	Fields []Field // the target's column fields
	PK     Field
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
		if _, ok := f.Tag.Lookup(relation.TagKey); ok {
			return []diag.Diag{diag.Warningf(s.Pos,
				"%s has orm tags but no //kanna:table directive, so kanna-orm ignores it", s.Name)}
		}
	}
	return nil
}

func buildTable(s ir.Struct, args string) (*Table, []diag.Diag) {
	var diags []diag.Diag

	if !token.IsExported(s.Name) {
		return nil, []diag.Diag{diag.Errorf(s.Pos,
			"%s is unexported; the generated package cannot reference it", s.Name)}
	}
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

	for _, f := range s.Fields {
		if f.Embedded {
			diags = append(diags, diag.Warningf(f.Pos,
				"embedded field %s is ignored; declare its columns on %s directly", f.Name, s.Name))
			continue
		}
		if !f.Exported {
			continue
		}

		raw, hasTag := f.Tag.Lookup(relation.TagKey)
		col, rel, errs := relation.ParseTag(raw)
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

		field, ok := buildField(f, col, hasTag)
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
	tokens := strings.Fields(args)
	k, v, found := strings.Cut(tokens[0], "=")
	if len(tokens) != 1 || !found || k != "name" || v == "" {
		return "", []diag.Diag{diag.Errorf(pos,
			"//kanna:table takes no argument or name=<table>, got %q", args)}
	}
	return v, nil
}

// buildField turns a column-tagged (or untagged) field into a Field. Untagged
// fields whose type reads as a relation — a slice of structs or a pointer to
// one — are not columns and are skipped, the same way a hand-written query
// would not select them; struct types that still read as columns (time.Time,
// anything a driver can hand a value through Scan) stay.
//
// Writing any orm tag turns the name-based timestamp inference off: a tag is
// an explicit statement of what the field is, so a field that wants both a
// custom column name and timestamp behavior says so with created_at or
// updated_at.
func buildField(f ir.Field, col *relation.ColumnTag, hasTag bool) (Field, bool) {
	if col.Skip {
		return Field{}, false
	}
	if !hasTag && relationShape(f.Type) {
		return Field{}, false
	}

	column := col.Column
	if column == "" {
		column = relation.SnakeCase(f.Name)
	}

	basic, isBasic := f.Type.Underlying().(*types.Basic)
	goType, typePkgs := renderType(f.Type)

	return Field{
		Name:       f.Name,
		Column:     column,
		GoType:     goType,
		IntKind:    isBasic && basic.Info()&types.IsInteger != 0,
		Comparable: types.Comparable(f.Type),
		TypePkgs:   typePkgs,
		PrimaryKey: col.PrimaryKey,
		CreatedAt:  col.CreatedAt || (!hasTag && f.Name == "CreatedAt"),
		UpdatedAt:  col.UpdatedAt || (!hasTag && f.Name == "UpdatedAt"),
		Pos:        f.Pos,
	}, true
}

// relationShape reports whether an untagged field's type looks like a
// relation rather than a column: a slice of structs or a pointer to one,
// unless the struct itself reads as a column.
func relationShape(t types.Type) bool {
	core := t
	if elem, isSlice := relation.SliceElem(core); isSlice {
		core = elem
	} else if elem, isPtr := relation.PointerElem(core); isPtr {
		core = elem
	} else {
		return false
	}
	if elem, isPtr := relation.PointerElem(core); isPtr {
		core = elem
	}
	named, ok := relation.StructNamed(core)
	return ok && !columnLike(named)
}

// columnLike reports whether a struct-kind named type still reads as a
// column: time.Time, or anything carrying a Scan method for the driver to
// hand a value through.
func columnLike(named *types.Named) bool {
	if obj := named.Obj(); obj.Name() == "Time" && obj.Pkg() != nil && obj.Pkg().Path() == "time" {
		return true
	}
	for m := range types.NewMethodSet(types.NewPointer(named)).Methods() {
		sig, ok := m.Obj().Type().(*types.Signature)
		if !ok || m.Obj().Name() != "Scan" || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
			continue
		}
		// The sql.Scanner contract exactly: Scan(any) error. Anything else
		// named Scan is not something database/sql can hand a value to.
		param, isInterface := sig.Params().At(0).Type().Underlying().(*types.Interface)
		if !isInterface || !param.Empty() {
			continue
		}
		if types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
			return true
		}
	}
	return false
}

func buildRelation(s ir.Struct, f ir.Field, rel *relation.RelationTag, srcPkg *types.Package) (*Relation, []diag.Diag) {
	core := f.Type
	elem, isSlice := relation.SliceElem(core)
	if isSlice {
		core = elem
	}
	elem, isPointer := relation.PointerElem(core)
	if isPointer {
		core = elem
	}

	named, ok := relation.StructNamed(core)
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
	if isSlice && isPointer {
		// The generated preloader builds []T and assigns it to the field, so
		// a pointer element would be a type error far from this tag.
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: a %s field must be a slice of structs; pointer elements are not supported",
			s.Name, f.Name, rel.Kind)}
	}

	if p := named.Obj().Pkg(); p != nil && p != srcPkg {
		// The target's table name, primary key, and factory location are the
		// business of its own generation run; a single-package run cannot see
		// its directive or where its queries land, so guessing would emit
		// imports and names that may not exist.
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: relation target %s lives in %s; relations resolve within one package",
			s.Name, f.Name, named.Obj().Name(), p.Path()).
			WithHints("declare the relation in the target's package, or mirror the type locally")}
	}

	return &Relation{
		FieldName:  f.Name,
		Kind:       rel.Kind,
		TargetType: named.Obj().Name(),
		ForeignKey: rel.ForeignKey,
		JoinTable:  rel.JoinTable,
		References: rel.References,
		IsPointer:  isPointer,
		Pos:        f.Pos,
	}, nil
}

// resolvePK settles the primary key through the shared rule: an explicit
// primary_key option wins, a field named ID is the fallback, anything else is
// an error. Two explicit keys are two mistakes with positions, not a silent
// pick.
func resolvePK(table *Table, s ir.Struct) []diag.Diag {
	candidates := make([]relation.PKCandidate, len(table.Fields))
	for i, f := range table.Fields {
		candidates[i] = relation.PKCandidate{Name: f.Name, Explicit: f.PrimaryKey}
	}

	idx, dupes := relation.PickPrimaryKey(candidates)
	if len(dupes) > 0 {
		diags := make([]diag.Diag, 0, len(dupes))
		for _, i := range dupes {
			diags = append(diags, diag.Errorf(table.Fields[i].Pos,
				"%s has more than one primary_key", s.Name))
		}
		return diags
	}
	if idx < 0 {
		return []diag.Diag{diag.Errorf(s.Pos,
			"%s has no primary key; name a field ID or tag one with orm:\",primary_key\"", s.Name)}
	}

	table.Fields[idx].PrimaryKey = true
	return nil
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
			diags = append(diags, resolveLocal(t, &t.Relations[ri], byName, marked, inPackage)...)
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
	r.TargetKeyType = target.PK().GoType
	r.TypeDeps = append(r.TypeDeps, target.PK().TypePkgs...)

	// Every kind keys a map (or QueryJoinTable's comparable parameters) by
	// these types, so []byte and friends have to be caught here rather than
	// as a compile error in the output.
	for _, pk := range []Field{t.PK(), target.PK()} {
		if !pk.Comparable {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: primary key type %s is not comparable, and preloading keys maps by it",
				t.Name, r.FieldName, pk.GoType)}
		}
	}

	switch r.Kind {
	case "belongs_to":
		fk := columnField(*t, r.ForeignKey)
		if fk == nil {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s", t.Name, r.FieldName, r.ForeignKey, t.Name)}
		}
		r.ForeignKeyField = fk.Name
		r.KeyType, r.FKIsPointer = derefType(fk.GoType)
		if r.KeyType != r.TargetKeyType {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %s.%s has type %s, but the %s primary key is %s",
				t.Name, r.FieldName, t.Name, fk.Name, fk.GoType, r.TargetType, r.TargetKeyType)}
		}
		r.TypeDeps = append(r.TypeDeps, fk.TypePkgs...)
	case "has_many", "has_one":
		fk := columnField(*target, r.ForeignKey)
		if fk == nil {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %q is not a column of %s", t.Name, r.FieldName, r.ForeignKey, r.TargetType)}
		}
		r.ForeignKeyField = fk.Name
		keyType, fkPtr := derefType(fk.GoType)
		if keyType != t.PK().GoType {
			return []diag.Diag{diag.Errorf(r.Pos,
				"%s.%s: foreign_key %s.%s has type %s, but the %s primary key is %s",
				t.Name, r.FieldName, r.TargetType, fk.Name, fk.GoType, t.Name, t.PK().GoType)}
		}
		r.FKIsPointer = fkPtr
		r.KeyType = keyType
		r.TypeDeps = append(r.TypeDeps, t.PK().TypePkgs...)
	default: // many_to_many
		r.KeyType = t.PK().GoType
		r.TypeDeps = append(r.TypeDeps, t.PK().TypePkgs...)
	}

	if r.Kind == "belongs_to" || r.Kind == "has_one" {
		r.JoinScan = &JoinScan{Fields: target.Fields, PK: target.PK()}
	}
	return nil
}

// renderType renders t qualified by package name — the generated file lives
// in another package, so every named type carries its package — collecting
// the packages the string mentions.
func renderType(t types.Type) (string, []PkgRef) {
	var deps []PkgRef
	seen := map[string]bool{}
	s := types.TypeString(t, func(p *types.Package) string {
		if !seen[p.Path()] {
			seen[p.Path()] = true
			deps = append(deps, PkgRef{Name: p.Name(), Path: p.Path()})
		}
		return p.Name()
	})
	return s, deps
}

// derefType splits a possibly-pointer type string into its element and
// whether it was a pointer.
func derefType(goType string) (string, bool) {
	if len(goType) > 0 && goType[0] == '*' {
		return goType[1:], true
	}
	return goType, false
}

func columnField(t Table, column string) *Field {
	for i, f := range t.Fields {
		if f.Column == column {
			return &t.Fields[i]
		}
	}
	return nil
}
