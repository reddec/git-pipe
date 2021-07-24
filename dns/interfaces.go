package dns

import (
	"context"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
)

// DNS records management.
type DNS interface {
	// Register (updated or add) domains to current IP.
	Register(ctx context.Context, domains []string) error
}

func Daemonize(provider DNS) core.Daemon {
	const bufferSize = 1024
	return core.FuncDaemon(func(ctx context.Context, environment core.DaemonEnvironment) error {
		ch := environment.Global().Registry().Subscribe(bufferSize, true)
		defer environment.Global().Registry().Unsubscribe(ch)
		logger := internal.LoggerFromContext(ctx).Named("dns")

		environment.Ready()

		for event := range ch {
			if event.Event != core.RegistryEventRegistered {
				continue
			}
			logger = logger.With(zap.String("domain", event.Service.Domain),
				zap.String("service", event.Service.Name),
				zap.String("namespace", event.Service.Namespace))
			logger.Debug("registering domain")
			if err := provider.Register(ctx, []string{event.Service.Domain}); err != nil {
				logger.Error("failed register domain", zap.Error(err))
			}
		}
		return nil
	})
}
