package locale

import (
	"fmt"
	"slices"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

// ParseTOML parses TOML locale data.
func ParseTOML(tag language.Tag, data []byte) (Catalog, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Catalog{}, fmt.Errorf("parse toml: %w", err)
	}
	n, err := tomlNode("", raw)
	if err != nil {
		return Catalog{}, err
	}
	return catalogFrom(tag, n)
}

// tomlNode converts a decoded TOML table. TOML carries no position info, so
// errors identify values by key path instead of line.
func tomlNode(path string, raw map[string]any) (node, error) {
	n := node{mapping: true}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		keyPath := k
		if path != "" {
			keyPath = path + "." + k
		}
		var v node
		switch value := raw[k].(type) {
		case string:
			v = node{str: value}
		case map[string]any:
			sub, err := tomlNode(keyPath, value)
			if err != nil {
				return node{}, err
			}
			v = sub
		default:
			return node{}, fmt.Errorf("key %q: value must be a string or a table, got %T", keyPath, value)
		}
		n.children = append(n.children, child{key: k, node: v})
	}
	return n, nil
}
