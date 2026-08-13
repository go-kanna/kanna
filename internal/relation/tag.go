// Package relation holds the interpretation of orm tags that more than one
// generator relies on: the tag grammar, the mechanical name conversion, and
// the primary-key rule. kanna-orm consumes it for query generation and
// kanna-fixture for foreign-key-consistent fixtures, so the rules cannot
// drift apart. The relation graph itself (traversal order, cycle detection)
// arrives with its first consumer.
package relation

import (
	"fmt"
	"strings"
)

// TagKey is the struct tag every kanna-orm annotation lives under.
const TagKey = "orm"

// relationKinds are the reserved words that make an orm tag a relation. A tag
// whose first element is anything else names a column.
var relationKinds = map[string]bool{
	"has_many":     true,
	"has_one":      true,
	"belongs_to":   true,
	"many_to_many": true,
}

// ColumnTag is the parsed form of an orm tag on a column field. The zero value
// stands for an untagged field: everything inferred.
type ColumnTag struct {
	Column     string // explicit column name; empty means infer from the field name
	Skip       bool   // orm:"-"
	PrimaryKey bool
	CreatedAt  bool
	UpdatedAt  bool
}

// RelationTag is the parsed form of an orm tag on a relation field.
type RelationTag struct {
	Kind       string
	ForeignKey string
	JoinTable  string // many_to_many only
	References string // many_to_many only
}

// ParseTag interprets one orm tag value. Exactly one of the returned structs
// is non-nil when errs is empty. Every malformed element is reported with its
// exact spelling, because a typo silently ignored is a column or relation
// silently misconfigured.
func ParseTag(value string) (*ColumnTag, *RelationTag, []string) {
	if value == "-" {
		return &ColumnTag{Skip: true}, nil, nil
	}

	parts := strings.Split(value, ",")
	if relationKinds[parts[0]] {
		rel, errs := parseRelationTag(parts[0], parts[1:])
		return nil, rel, errs
	}
	col, errs := parseColumnTag(parts[0], parts[1:])
	return col, nil, errs
}

func parseColumnTag(name string, opts []string) (*ColumnTag, []string) {
	col := ColumnTag{Column: name}
	var errs []string

	if name == "-" {
		errs = append(errs, `"-" cannot be combined with other options`)
	}

	for _, opt := range opts {
		switch opt {
		case "primary_key":
			col.PrimaryKey = true
		case "created_at":
			col.CreatedAt = true
		case "updated_at":
			col.UpdatedAt = true
		case "":
			errs = append(errs, "empty option in orm tag")
		default:
			errs = append(errs, fmt.Sprintf("unknown option %q in orm tag", opt))
		}
	}

	return &col, errs
}

func parseRelationTag(kind string, opts []string) (*RelationTag, []string) {
	rel := RelationTag{Kind: kind}
	var errs []string

	seen := make(map[string]bool, len(opts))
	for _, opt := range opts {
		k, v, found := strings.Cut(opt, ":")
		if !found || v == "" {
			errs = append(errs, fmt.Sprintf("relation option %q must take the form key:value", opt))
			continue
		}
		if seen[k] {
			errs = append(errs, fmt.Sprintf("duplicate relation option %q", k))
			continue
		}
		seen[k] = true

		switch k {
		case "foreign_key":
			rel.ForeignKey = v
		case "join_table":
			rel.JoinTable = v
		case "references":
			rel.References = v
		default:
			errs = append(errs, fmt.Sprintf("unknown relation option %q in orm tag", k))
		}
	}

	if rel.ForeignKey == "" {
		errs = append(errs, kind+" requires foreign_key:<column>")
	}
	if kind == "many_to_many" {
		if rel.JoinTable == "" {
			errs = append(errs, "many_to_many requires join_table:<table>")
		}
		if rel.References == "" {
			errs = append(errs, "many_to_many requires references:<column>")
		}
	} else {
		if rel.JoinTable != "" {
			errs = append(errs, "join_table is only valid for many_to_many")
		}
		if rel.References != "" {
			errs = append(errs, "references is only valid for many_to_many")
		}
	}

	return &rel, errs
}
