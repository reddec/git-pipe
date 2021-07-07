package internal

import (
	"path/filepath"
	"strings"
)

func ToDomain(file string) string {
	return strings.ToLower(strings.ReplaceAll(filepath.Base(file), "_", "-"))
}
