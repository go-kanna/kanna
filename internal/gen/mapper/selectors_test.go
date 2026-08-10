package mapper_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/mapper"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, env mapper.Env) mapper.ImportScope {
	t.Helper()
	scope, err := mapper.CollectImports(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return scope
}

func TestResolveSelectorGoFilePriority(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import model "example.com/a/model"
`)
	writeFile(t, dir, "other.go", `package handler

import model "example.com/b/model"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	got, err := scope.ResolveSelector("model", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/a/model" {
		t.Errorf("got %q, want %q", got, "example.com/a/model")
	}
}

func TestResolveSelectorUnnamedImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import (
	"example.com/gen/employee/v1"
	"example.com/internal/model"
)
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	pkgNames := map[string]string{
		"example.com/gen/employee/v1": "employeev1",
		"example.com/internal/model":  "model",
	}

	got, err := scope.ResolveSelector("employeev1", pkgNames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/gen/employee/v1" {
		t.Errorf("got %q, want %q", got, "example.com/gen/employee/v1")
	}
}

func TestResolveSelectorBlankImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import _ "example.com/lib/converters"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	pkgNames := map[string]string{"example.com/lib/converters": "converters"}

	got, err := scope.ResolveSelector("converters", pkgNames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/lib/converters" {
		t.Errorf("got %q, want %q", got, "example.com/lib/converters")
	}
}

func TestResolveSelectorDotImportIgnored(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import . "example.com/dot"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	if _, err := scope.ResolveSelector("dot", map[string]string{"example.com/dot": "dot"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveSelectorAmbiguous(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "a.go", `package handler

import pb "example.com/a/pb"
`)
	writeFile(t, dir, "b.go", `package handler

import pb "example.com/b/pb"
`)

	scope := collect(t, mapper.Env{GoPackage: "handler", Dir: dir})
	_, err := scope.ResolveSelector("pb", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error %q does not contain %q", err, "ambiguous")
	}
}

func TestResolveSelectorNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	_, err := scope.ResolveSelector("model", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot resolve package selector") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCollectImportsSkipsTestFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import pb "example.com/a/pb"
`)
	writeFile(t, dir, "handler_test.go", `package handler

import pb "example.com/b/pb"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	got, err := scope.ResolveSelector("pb", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/a/pb" {
		t.Errorf("got %q, want %q", got, "example.com/a/pb")
	}
}

func TestCollectImportsSkipsOtherPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import pb "example.com/a/pb"
`)
	writeFile(t, dir, "tool.go", `package main

import pb "example.com/b/pb"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	got, err := scope.ResolveSelector("pb", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/a/pb" {
		t.Errorf("got %q, want %q", got, "example.com/a/pb")
	}
}

func TestCollectImportsFiltersByGoFilePackageClause(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler
`)
	writeFile(t, dir, "other.go", `package handler

import pb "example.com/a/pb"
`)
	writeFile(t, dir, "tool.go", `package main

import pb "example.com/b/pb"
`)

	// GOPACKAGE is unset: the filter must come from handler.go's package
	// clause, or tool.go's import would make pb ambiguous.
	scope := collect(t, mapper.Env{GoFile: "handler.go", Dir: dir})
	got, err := scope.ResolveSelector("pb", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/a/pb" {
		t.Errorf("got %q, want %q", got, "example.com/a/pb")
	}
}

func TestCollectImportsWithoutGoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import model "example.com/internal/model"
`)

	scope := collect(t, mapper.Env{Dir: dir})
	got, err := scope.ResolveSelector("model", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com/internal/model" {
		t.Errorf("got %q, want %q", got, "example.com/internal/model")
	}
}

func TestCollectImportsMultiplePackagesWithoutEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import pb "example.com/a/pb"
`)
	writeFile(t, dir, "tool.go", `package main

import pb "example.com/b/pb"
`)

	_, err := mapper.CollectImports(mapper.Env{Dir: dir})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "multiple packages (handler, main)") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCollectImportsMissingGoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler
`)

	_, err := mapper.CollectImports(mapper.Env{GoFile: "missing.go", GoPackage: "handler", Dir: dir})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCollectImportsParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "broken.go", `package handler

import (
`)

	_, err := mapper.CollectImports(mapper.Env{GoPackage: "handler", Dir: dir})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestImportPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import (
	"example.com/b/model"
	pb "example.com/a/pb"

	_ "example.com/a/blank"
)
`)
	writeFile(t, dir, "other.go", `package handler

import "example.com/b/model"
`)

	scope := collect(t, mapper.Env{GoFile: "handler.go", GoPackage: "handler", Dir: dir})
	want := []string{"example.com/a/blank", "example.com/a/pb", "example.com/b/model"}
	if got := scope.ImportPaths(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
