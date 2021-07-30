package dummy

import (
	"context"

	"github.com/reddec/git-pipe/core"
)

// New dummy ingress which does nothing.
func New() core.Ingress {
	return &dummyRouter{}
}

type dummyRouter struct{}

func (dr *dummyRouter) Clear(ctx context.Context, group string) error {
	return nil
}

func (dr *dummyRouter) Set(ctx context.Context, group string, domainAddresses map[string][]string) error {
	return nil
}
