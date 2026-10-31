package safepath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsSiblingPrefix(t *testing.T) {
	root := t.TempDir()
	sibling := root + "-old"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, ok := ResolveIn(filepath.Join(sibling, "secret.txt"), []string{root}); ok {
		t.Fatal("path outside the root was accepted")
	}
}

func TestResolveAcceptsNestedPath(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sub", "file.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, ok := ResolveIn(nested, []string{root})
	if !ok {
		t.Fatal("nested path was rejected")
	}
	if resolved == "" {
		t.Fatal("resolved path is empty")
	}
}

func TestResolveRejectsParentTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "..", "outside.txt")

	if _, ok := ResolveIn(outside, []string{root}); ok {
		t.Fatal("parent traversal was accepted")
	}
}

func TestResolveRejectsEmpty(t *testing.T) {
	if _, ok := ResolveIn("   ", []string{t.TempDir()}); ok {
		t.Fatal("empty path was accepted")
	}
}

func TestResolveRejectsSymlinkWithMissingDescendant(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	target := filepath.Join(link, "newdir", "file.txt")
	if _, ok := ResolveIn(target, []string{root}); ok {
		t.Fatal("path below a symlink with missing descendants was accepted")
	}
}
