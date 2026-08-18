package relation

import (
	"go/token"
	"go/types"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
)

// TableFields classifies the fields of one //kanna:table struct.
type TableFields struct {
	Columns   []string // column-backed field names, the row's data
	Relations []string // relation-tagged field names, every kind

	// Broken marks a table with a malformed tag: the lists are incomplete,
	// and a consumer must not draw completeness conclusions from them.
	Broken bool
}

// TableSet indexes the classified tables of one or more packages by package
// path and type name, so a consumer holding a types.Named can look its
// table up.
type TableSet map[string]TableFields

// Of returns the classification of the named type, when it is a table.
func (s TableSet) Of(named *types.Named) (TableFields, bool) {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return TableFields{}, false
	}
	tf, ok := s[obj.Pkg().Path()+"."+obj.Name()]
	return tf, ok
}

// Tables classifies every //kanna:table struct among the scanned structs: the
// read-only view generators other than the orm need, saying which fields
// carry the row's data and which are relations. Malformed tags are positioned
// errors; a consumer that does not own tag enforcement demotes them. Structs
// the orm rejects as tables — unexported or generic — are not classified;
// rejecting them is the orm's business.
func Tables(structs []ir.Struct) (TableSet, []diag.Diag) {
	set := make(TableSet)
	var diags []diag.Diag

	for _, s := range structs {
		d, msgs := directive.Find(s.Doc, TableDirective)
		diags = append(diags, msgs.Diags(s.Pos)...)
		if !d.Found {
			continue
		}
		if !token.IsExported(s.Name) || (s.Named != nil && s.Named.TypeParams().Len() > 0) {
			continue
		}

		c := classifyTable(s)
		tf := TableFields{Broken: c.broken}
		for _, cf := range c.fields {
			diags = append(diags, cf.diags...)
			switch {
			case cf.column != nil:
				tf.Columns = append(tf.Columns, cf.field.Name)
			case cf.relation != nil:
				tf.Relations = append(tf.Relations, cf.field.Name)
			}
		}
		set[s.PkgPath+"."+s.Name] = tf
	}

	return set, diags
}
