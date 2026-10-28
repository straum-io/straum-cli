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
// generates. make docs (go test -run TestCommandDocs -update) rewrites it.
func TestCommandDocs(t *testing.T) {
	root := newRoot(&env{getenv: func(string) string { return "" }})
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
	names := func(glob string) []string {
		paths, _ := filepath.Glob(glob)
		for i, p := range paths {
			paths[i] = filepath.Base(p)
		}
		return paths
	}
	want, have := names(filepath.Join(dir, "*.md")), names(filepath.Join(commandDocs, "*.md"))
	if !slices.Equal(want, have) {
		t.Fatalf("%s has %v, the commands generate %v; run make docs", commandDocs, have, want)
	}
	for _, name := range want {
		gen, _ := os.ReadFile(filepath.Join(dir, name))
		cur, _ := os.ReadFile(filepath.Join(commandDocs, name))
		if !bytes.Equal(gen, cur) {
			t.Errorf("%s/%s is stale; run make docs", commandDocs, name)
		}
	}
}
