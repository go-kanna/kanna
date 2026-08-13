package relation

// PKCandidate is what the primary-key rule needs to know about one field.
type PKCandidate struct {
	Name     string
	Explicit bool // tagged with primary_key
}

// PickPrimaryKey applies the shared primary-key rule: exactly one explicit
// primary_key wins, and a field named ID is the fallback. It returns the
// chosen index and nil; -1 and nil when nothing qualifies; or -1 and every
// explicit index when several fields claim the key, so the caller can report
// each with its position instead of silently picking one.
func PickPrimaryKey(fields []PKCandidate) (int, []int) {
	var explicit []int
	for i, f := range fields {
		if f.Explicit {
			explicit = append(explicit, i)
		}
	}

	switch len(explicit) {
	case 1:
		return explicit[0], nil
	case 0:
		for i, f := range fields {
			if f.Name == "ID" {
				return i, nil
			}
		}
		return -1, nil
	default:
		return -1, explicit
	}
}
