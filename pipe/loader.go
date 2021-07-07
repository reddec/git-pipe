package pipe

import (
	"os"
	"path/filepath"
)

func hasAnyFile(root string, files ...string) bool {
	for _, file := range files {
		if f, err := os.Stat(filepath.Join(root, file)); err == nil && !f.IsDir() {
			return true
		}
	}

	return false
}
