package relation

import (
	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
)

// TableFields classifies the fields of one //kanna:table struct by what the
// orm tags say they are.
type TableFields struct {
	Columns   []string // column-backed field names, the row's data
	Relations []string // relation-tagged field names, every kind
}

// Tables classifies every //kanna:table struct in the scanned package. It is
// the read-only view generators other than the orm need: which fields carry
// the row's data and which are relations. Malformed tags are positioned
// errors; a consumer that does not own tag enforcement demotes them.
func Tables(structs []ir.Struct) (map[string]TableFields, []diag.Diag) {
	out := make(map[string]TableFields)
	var diags []diag.Diag

	for _, s := range structs {
		d, _ := directive.Find(s.Doc, "table")
		if !d.Found {
			continue
		}

		c := classifyTable(s)
		diags = append(diags, c.diags...)

		var tf TableFields
		for _, col := range c.columns {
			tf.Columns = append(tf.Columns, col.field.Name)
		}
		for _, r := range c.relations {
			tf.Relations = append(tf.Relations, r.field.Name)
		}
		out[s.Name] = tf
	}

	return out, diags
}

// relField is one relation-tagged field with its parsed tag.
type relField struct {
	field ir.Field
	tag   *RelationTag
}

// classifiedTable is the shared interpretation of one table struct's fields,
// which both the graph builder and Tables read.
type classifiedTable struct {
	columns   []column
	relations []relField
	broken    bool
	diags     []diag.Diag
}

// classifyTable splits a table struct's exported, non-embedded fields into
// columns and relation fields, parsing each orm tag exactly once.
func classifyTable(s ir.Struct) classifiedTable {
	var c classifiedTable

	for _, f := range s.Fields {
		if !f.Exported || f.Embedded {
			continue
		}

		raw, hasTag := f.Tag.Lookup(TagKey)
		col, rel, errs := ParseTag(raw)
		for _, e := range errs {
			c.diags = append(c.diags, diag.Errorf(f.Pos, "%s.%s: %s", s.Name, f.Name, e))
		}
		if len(errs) > 0 {
			c.broken = true
			continue
		}

		if rel != nil {
			c.relations = append(c.relations, relField{field: f, tag: rel})
			continue
		}

		if col.Skip || (!hasTag && RelationShape(f.Type)) {
			continue
		}
		name := col.Column
		if name == "" {
			name = SnakeCase(f.Name)
		}
		c.columns = append(c.columns, column{field: f, name: name, explicitPK: col.PrimaryKey})
	}

	return c
}
