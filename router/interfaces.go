package router

import (
	"context"

	"github.com/reddec/git-pipe/packs"
)

// Router to internal exposed services.
type Router interface {
	// Update routing table for group.
	Update(ctx context.Context, group string, services []packs.Service) error
}
