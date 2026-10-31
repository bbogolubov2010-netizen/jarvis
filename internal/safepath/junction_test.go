package safepath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveRejectsJunctionWithMissingDescendant(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction points are Windows-specific")
	}
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "junction")
	if err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).Run(); err != nil {
		t.Skipf("junction creation is not available: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	target := filepath.Join(link, "created-by-jarvis.txt")
	if got, ok := ResolveIn(target, []string{root}); ok {
		t.Fatalf("path through a junction was accepted: %q", got)
	}
}
