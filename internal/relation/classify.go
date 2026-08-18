package relation

import (
	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
)

// TableDirective is the directive key that opts a struct in as a table.
const TableDirective = "table"

// classifiedField is one field with its disposition. Exactly one of column
// and relation is set for a classified field; neither means the field is
// skipped (orm:"-", an untagged field shaped like a relation, or an embedded
// field, which carries only its warning). diags hold whatever the field has
// to say — malformed tags, the embedded warning — in declaration order.
type classifiedField struct {
	field    ir.Field
	column   *column
	relation *RelationTag
	diags    []diag.Diag
}

// classifiedTable is the shared interpretation of one table struct's fields,
// in declaration order. It exists so every generator that reads orm tags —
// the graph builder today, the mapper integration next — agrees on what a
// table's fields are without parsing anything twice.
type classifiedTable struct {
	fields []classifiedField
	broken bool // a tag failed to parse; the column list is incomplete
}

// classifyTable interprets a table struct's fields, parsing each orm tag
// exactly once. Unexported fields are not classifiable and carry no entry;
// embedded fields warn, the same way the orm generator does.
func classifyTable(s ir.Struct) classifiedTable {
	var c classifiedTable

	for _, f := range s.Fields {
		if f.Embedded {
			c.fields = append(c.fields, classifiedField{field: f, diags: []diag.Diag{diag.Warningf(f.Pos,
				"embedded field %s is ignored; declare its columns on %s directly", f.Name, s.Name)}})
			continue
		}
		if !f.Exported {
			continue
		}

		raw, hasTag := f.Tag.Lookup(TagKey)
		col, rel, errs := ParseTag(raw)
		if len(errs) > 0 {
			cf := classifiedField{field: f}
			for _, e := range errs {
				cf.diags = append(cf.diags, diag.Errorf(f.Pos, "%s.%s: %s", s.Name, f.Name, e))
			}
			c.fields = append(c.fields, cf)
			c.broken = true
			continue
		}

		if rel != nil {
			c.fields = append(c.fields, classifiedField{field: f, relation: rel})
			continue
		}

		if col.Skip || (!hasTag && RelationShape(f.Type)) {
			continue
		}
		name := col.Column
		if name == "" {
			name = SnakeCase(f.Name)
		}
		c.fields = append(c.fields, classifiedField{
			field:  f,
			column: &column{field: f, name: name, explicitPK: col.PrimaryKey},
		})
	}

	return c
}

// columns returns the column-backed entries in declaration order.
func (c classifiedTable) columns() []column {
	var out []column
	for _, cf := range c.fields {
		if cf.column != nil {
			out = append(out, *cf.column)
		}
	}
	return out
}

// column is one column-backed field with its resolved column name.
type column struct {
	field      ir.Field
	name       string
	explicitPK bool
}

// columnNamed finds the column carrying the given column name.
func columnNamed(columns []column, name string) (column, bool) {
	for _, c := range columns {
		if c.name == name {
			return c, true
		}
	}
	return column{}, false
}
