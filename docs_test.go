package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/cobra/doc"
)

const commandDocs = "docs/commands"

// TestCommandDocs fails when docs/commands is not what the command tree
// generates. `make docs` (go test -run TestCommandDocs -update) rewrites it.
func TestCommandDocs(t *testing.T) {
	root := newRoot(func(string) string { return "" })
	root.DisableAutoGenTag = true // no date: the output depends only on the code
	dir := t.TempDir()
	if err := doc.GenMarkdownTree(root, dir); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.RemoveAll(commandDocs); err != nil {
			t.Fatal(err)
		}
		if err := os.CopyFS(commandDocs, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
	}
	want, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	have, _ := filepath.Glob(filepath.Join(commandDocs, "*.md"))
	names := func(paths []string) []string {
		for i, p := range paths {
			paths[i] = filepath.Base(p)
		}
		return paths
	}
	if w, h := names(want), names(have); !slices.Equal(w, h) {
		t.Fatalf("%s has %v, the commands generate %v; run make docs", commandDocs, h, w)
	}
	for _, name := range names(want) {
		gen, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		cur, err := os.ReadFile(filepath.Join(commandDocs, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gen, cur) {
			t.Errorf("%s/%s is stale; run make docs", commandDocs, name)
		}
	}
}
