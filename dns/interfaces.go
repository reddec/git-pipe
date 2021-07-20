package dns

import (
	"context"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
)

// DNS records management.
type DNS interface {
	// Register (updated or add) domains to current IP.
	Register(ctx context.Context, domains []string) error
}

func Daemonize(provider DNS) core.Daemon {
	const bufferSize = 1024
	return core.FuncDaemon(func(ctx context.Context, environment core.DaemonEnvironment) {
		ch := environment.Environment.Registry().Subscribe(bufferSize, true)
		defer environment.Environment.Registry().Unsubscribe(ch)

		logger := internal.LoggerFromContext(ctx)

		for event := range ch {
			if event.Event != core.RegistryEventRegistered {
				continue
			}

			if err := provider.Register(ctx, []string{event.Service.Domain}); err != nil {
				logger.Println("failed register domain", event.Service.Domain, "for service", event.Service.Label(), ":", err)
			}
		}
	})
}
