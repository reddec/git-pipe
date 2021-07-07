package remote

import (
	"context"
	"net/url"
)

// Source provider for remote repository.
type Source interface {
	// Ref to repository.
	Ref() url.URL
	// Poll repository for changes. Should return true without error if something changed.
	Poll(ctx context.Context, targetDir string) (bool, error)
}
