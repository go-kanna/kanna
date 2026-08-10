package mapper

import (
	"go/types"
	"io"
)

// Parse exposes parseFlags for tests, dropping the version flag it also reports.
func Parse(args []string, stderr io.Writer) (Config, error) {
	cfg, _, err := parseFlags(args, stderr)

	return cfg, err
}

// ImportScope exposes importScope for tests.
type ImportScope = importScope

// CollectImports exposes collectImports for tests.
var CollectImports = collectImports

// ResolveSelector exposes importScope.resolveSelector for tests.
func (s importScope) ResolveSelector(sel string, pkgNames map[string]string) (string, error) {
	return s.resolveSelector(sel, pkgNames)
}

// ImportPaths exposes importScope.importPaths for tests.
func (s importScope) ImportPaths() []string {
	return s.importPaths()
}

// ConverterTable exposes converterTable for tests.
type ConverterTable = converterTable

// ExtractConverters exposes extractConverters for tests.
var ExtractConverters = extractConverters

// PairSpec exposes pairSpec for tests.
type PairSpec = pairSpec

// FieldKey exposes fieldKey for tests.
type FieldKey = fieldKey

// ResolveConfig exposes resolveConfig for tests.
type ResolveConfig = resolveConfig

// FuncPlan exposes funcPlan for tests.
type FuncPlan = funcPlan

// ResolvePlans exposes resolvePlans for tests.
var ResolvePlans = resolvePlans

// TypeConvertible exposes typeConvertible for tests.
var TypeConvertible = typeConvertible

// DescribePlan exposes funcPlan.describe for tests.
func DescribePlan(p *funcPlan) string {
	return p.describe()
}

// EmitFile exposes emitFile for tests.
var EmitFile = emitFile

// TrackerNames runs qualifier over pkgs in order and returns the local
// names assigned, for import collision tests.
func TrackerNames(outputPkgPath string, pkgs ...*types.Package) []string {
	tracker := newImportTracker(outputPkgPath)
	names := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		names = append(names, tracker.Qualifier(p))
	}
	return names
}

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
