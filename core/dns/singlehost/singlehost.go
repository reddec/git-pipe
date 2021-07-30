package singlehost

import (
	"context"
	"fmt"
	"sync"

	"github.com/reddec/git-pipe/core"
)

// New wrapper around DNS provider which registers only one root domain.
func New(rootDomain string, provider core.DNS) core.DNS {
	return &singleHost{
		rootDomain: rootDomain,
		provider:   provider,
	}
}

type singleHost struct {
	rootDomain string
	provider   core.DNS
	lock       sync.Mutex
	registered bool
}

func (sh *singleHost) Register(ctx context.Context, domains []string) error {
	if sh.registered {
		return nil
	}
	sh.lock.Lock()
	defer sh.lock.Unlock()
	if sh.registered {
		return nil
	}

	if err := sh.provider.Register(ctx, []string{sh.rootDomain}); err != nil {
		return fmt.Errorf("register root domain: %w", err)
	}

	sh.registered = true

	return nil
}
