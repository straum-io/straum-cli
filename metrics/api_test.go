package metrics

import (
	"bytes"
	"flag"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// exportedAPI prints every exported declaration of this package, without
// comments or function bodies: what another module can depend on (ECO-D-5).
func exportedAPI(t *testing.T) []byte {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0) // 0: comments dropped
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	pkg, err := doc.NewFromFiles(fset, files, "github.com/straum-io/straum-cli/metrics")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	decl := func(d ast.Decl) {
		if f, ok := d.(*ast.FuncDecl); ok {
			f.Body = nil
		}
		var s strings.Builder
		if err := printer.Fprint(&s, fset, d); err != nil {
			t.Fatal(err)
		}
		// The dropped comments leave blank lines; without them a comment
		// edit does not change the listing.
		for line := range strings.Lines(s.String()) {
			if strings.TrimSpace(line) != "" {
				b.WriteString(strings.TrimSuffix(line, "\n") + "\n")
			}
		}
		b.WriteString("\n")
	}
	values := func(vs []*doc.Value) {
		for _, v := range vs {
			decl(v.Decl)
		}
	}
	funcs := func(fs []*doc.Func) {
		for _, f := range fs {
			decl(f.Decl)
		}
	}
	values(pkg.Consts)
	values(pkg.Vars)
	funcs(pkg.Funcs)
	for _, typ := range pkg.Types {
		decl(typ.Decl)
		values(typ.Consts)
		values(typ.Vars)
		funcs(typ.Funcs)
		funcs(typ.Methods)
	}
	return b.Bytes()
}

// TestGoldenAPI fails on any change to the exported API, compatible or
// not; the reviewer decides which it is.
func TestGoldenAPI(t *testing.T) {
	got := exportedAPI(t)
	const path = "../testdata/metrics-api.golden"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := fixture(t, "metrics-api.golden"); !bytes.Equal(got, []byte(want)) {
		t.Errorf("the exported API of metrics changed; if intended, run make golden, record it in CHANGELOG.md, and treat a removed or changed declaration as incompatible\ngot:\n%s", got)
	}
}
