package parse

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Supported reports whether a registered parser handles path's extension
// (multi-part extensions such as ".tar.gz" included)
func Supported(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	for ext := range ParserRegistry {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// CollectFiles expands sources (files, directories, glob patterns) into
// absolute file paths. Directories are walked recursively. Paths that don't
// exist or have no registered parser are returned as skipped.
func CollectFiles(sources []string) (accepted, skipped []string) {
	seen := make(map[string]bool)

	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		if Supported(abs) {
			accepted = append(accepted, abs)
		} else {
			skipped = append(skipped, p)
		}
	}

	for _, src := range sources {
		info, err := os.Stat(src)
		switch {
		case err == nil && info.IsDir():
			var found []string
			filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err == nil && d.Type().IsRegular() && Supported(p) {
					found = append(found, p)
				}
				return nil
			})
			sort.Strings(found)
			for _, p := range found {
				add(p)
			}

		case err == nil:
			add(src)

		case strings.ContainsAny(src, "*?["):
			matches, _ := filepath.Glob(src)
			sort.Strings(matches)
			n := 0
			for _, m := range matches {
				if fi, err := os.Stat(m); err == nil && fi.Mode().IsRegular() {
					add(m)
					n++
				}
			}
			if n == 0 {
				skipped = append(skipped, src)
			}

		default:
			skipped = append(skipped, src)
		}
	}
	return accepted, skipped
}
