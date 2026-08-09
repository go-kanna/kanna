package mapper

import "go/types"

// ConverterTable exposes converterTable for tests.
type ConverterTable = converterTable

// ExtractConverters exposes extractConverters for tests.
var ExtractConverters = extractConverters

// ConverterInfo summarizes a converter for test assertions.
type ConverterInfo struct {
	Func    string
	PkgPath string
	HasErr  bool
}

// LookupInfo exposes converterTable.lookup for tests.
func (t converterTable) LookupInfo(src, dst types.Type) (ConverterInfo, bool) {
	c, ok := t.lookup(src, dst)
	if !ok {
		return ConverterInfo{}, false
	}
	return ConverterInfo{Func: c.fn.Name(), PkgPath: c.fn.Pkg().Path(), HasErr: c.hasErr}, true
}

// Len reports the number of registered converters for tests.
func (t converterTable) Len() int {
	return len(t.converters)
}
