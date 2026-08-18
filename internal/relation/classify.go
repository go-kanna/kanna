package relation

import (
	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
)

// TableDirective is the directive key that opts a struct in as a table.
const TableDirective = "table"

// ClassifiedField is one field the classification has something to say
// about: a column (Column set), a relation (Relation set), a malformed tag
// (Diags only, and the table is marked broken), or an embedded field (its
// warning only). Fields with nothing to say — unexported ones, orm:"-", and
// untagged fields shaped like a relation — carry no entry at all.
type ClassifiedField struct {
	Field    ir.Field
	Column   *ColumnFacts
	Relation *RelationTag
	Diags    []diag.Diag
}

// ColumnFacts is the column half of a field's tag, with the name-based
// inferences applied: the resolved column name, whether the tag claims the
// primary key, and whether the column is an automatic timestamp. Writing any
// orm tag turns the name-based timestamp inference off: a tag is an explicit
// statement of what the field is.
type ColumnFacts struct {
	Name       string
	PrimaryKey bool
	CreatedAt  bool
	UpdatedAt  bool
}

// ClassifiedTable is the shared interpretation of one table struct's fields,
// in declaration order. It exists so every generator that reads orm tags —
// the query generator, the graph builder, the mapper integration — agrees on
// what a table's fields are without parsing anything twice.
type ClassifiedTable struct {
	Fields []ClassifiedField
	Broken bool // a tag failed to parse; the column list is incomplete
}

// ClassifyTable interprets a table struct's fields, parsing each orm tag
// exactly once. Unexported fields are not classifiable and carry no entry;
// embedded fields warn.
func ClassifyTable(s ir.Struct) ClassifiedTable {
	var c ClassifiedTable

	for _, f := range s.Fields {
		if f.Embedded {
			c.Fields = append(c.Fields, ClassifiedField{Field: f, Diags: []diag.Diag{diag.Warningf(f.Pos,
				"embedded field %s is ignored; declare its columns on %s directly", f.Name, s.Name)}})
			continue
		}
		if !f.Exported {
			continue
		}

		raw, hasTag := f.Tag.Lookup(TagKey)
		col, rel, errs := ParseTag(raw)
		if len(errs) > 0 {
			cf := ClassifiedField{Field: f}
			for _, e := range errs {
				cf.Diags = append(cf.Diags, diag.Errorf(f.Pos, "%s.%s: %s", s.Name, f.Name, e))
			}
			c.Fields = append(c.Fields, cf)
			c.Broken = true
			continue
		}

		if rel != nil {
			c.Fields = append(c.Fields, ClassifiedField{Field: f, Relation: rel})
			continue
		}

		if col.Skip || (!hasTag && RelationShape(f.Type)) {
			continue
		}
		name := col.Column
		if name == "" {
			name = SnakeCase(f.Name)
		}
		c.Fields = append(c.Fields, ClassifiedField{
			Field: f,
			Column: &ColumnFacts{
				Name:       name,
				PrimaryKey: col.PrimaryKey,
				CreatedAt:  col.CreatedAt || (!hasTag && f.Name == "CreatedAt"),
				UpdatedAt:  col.UpdatedAt || (!hasTag && f.Name == "UpdatedAt"),
			},
		})
	}

	return c
}

// columns returns the column-backed entries in declaration order, each field
// zipped with its column facts.
func (c ClassifiedTable) columns() []column {
	var out []column
	for _, cf := range c.Fields {
		if cf.Column != nil {
			out = append(out, column{field: cf.Field, name: cf.Column.Name, explicitPK: cf.Column.PrimaryKey})
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
