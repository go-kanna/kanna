package fixture

import (
	"go/types"
	"io"
)

var (
	NameExpr    = nameExpr
	FindGoMod   = findGoMod
	PackageName = packageName
)

func ParseFlags(args []string, stderr io.Writer) (Config, error) {
	cfg, _, err := parseFlags(args, stderr)

	return cfg, err
}

func TagExpr(tag string, typ types.Type, pkgPath, pkgName string) string {
	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName}

	return inf.tagExpr(tag, typ).expr
}

func TypeExpr(typ types.Type) string {
	return typeExpr(typ).expr
}
