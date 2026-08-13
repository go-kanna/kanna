package relation

// PKCandidate is what the primary-key rule needs to know about one field.
type PKCandidate struct {
	Name     string
	Explicit bool // tagged with primary_key
}

// PickPrimaryKey applies the shared primary-key rule: exactly one explicit
// primary_key wins, and a field named ID is the fallback. One index is the
// pick; several are the explicitly tagged fields, returned whole so the
// caller can report each with its position instead of silently choosing;
// none means no field qualifies.
func PickPrimaryKey(fields []PKCandidate) []int {
	var explicit []int
	for i, f := range fields {
		if f.Explicit {
			explicit = append(explicit, i)
		}
	}
	if len(explicit) > 0 {
		return explicit
	}

	for i, f := range fields {
		if f.Name == "ID" {
			return []int{i}
		}
	}
	return nil
}
