package fixture

import "go/types"

var NameExpr = nameExpr

func TagExpr(tag string, typ types.Type, pkgPath, pkgName string) string {
	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName}

	return inf.tagExpr(tag, typ).expr
}

func TypeExpr(typ types.Type) string {
	return typeExpr(typ).expr
}
