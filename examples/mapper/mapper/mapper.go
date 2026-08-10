package mapper

//go:generate go tool kanna-mapper -types=model.Employee:*employeev1.Employee,model.Address:*employeev1.Address -converter-pkg=../lib/converters -ignore=employeev1.Employee.Revision

import (
	_ "github.com/go-kanna/kanna/examples/mapper/gen/employeev1"
	_ "github.com/go-kanna/kanna/examples/mapper/model"
)
