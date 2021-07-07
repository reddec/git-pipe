package dummy

import (
	"context"

	"github.com/reddec/git-pipe/packs"
)

type Dummy struct{}

func (d Dummy) Update(ctx context.Context, group string, services []packs.Service) error {
	return nil
}
