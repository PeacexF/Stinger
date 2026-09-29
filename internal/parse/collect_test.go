package parse_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PeacexF/Stinger/internal/parse"
)

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.CSV", "sub/c.tar.gz", "sub/d.exe", "e.png"} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x@y.com"), 0o644)
	}

	accepted, skipped := parse.CollectFiles([]string{
		dir,                               // walked recursively, unsupported files ignored
		filepath.Join(dir, "a.txt"),       // duplicate of a walked file
		filepath.Join(dir, "e.png"),       // explicit unsupported file
		filepath.Join(dir, "*.txt"),       // glob
		filepath.Join(dir, "missing.txt"), // does not exist
		filepath.Join(dir, "*.nomatch"),   // glob with no matches
	})

	if len(accepted) != 3 {
		t.Errorf("accepted = %v", accepted)
	}
	if len(skipped) != 3 {
		t.Errorf("skipped = %v", skipped)
	}
}
