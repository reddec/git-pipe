package core

import (
	"context"
	"time"

	"github.com/reddec/git-pipe/internal"
)

// FuncDaemon is convenient wrapper for single function as daemon.
func FuncDaemon(handler func(ctx context.Context, environment DaemonEnvironment) error) Daemon {
	return &singleRun{
		fn: handler,
	}
}

type singleRun struct {
	fn func(ctx context.Context, environment DaemonEnvironment) error
}

func (sr *singleRun) Create(_ context.Context, _ DaemonEnvironment) error {
	return nil
}

func (sr *singleRun) Run(ctx context.Context, environment DaemonEnvironment) error {
	return sr.fn(ctx, environment)
}

func (sr *singleRun) Remove(_ context.Context, _ DaemonEnvironment) error {
	return nil
}

// FuncTimer is wrapper for single function that will be called again and again each interval regardless of error till
// context canceled. All errors will be logged.
func FuncTimer(interval time.Duration, handler func(ctx context.Context, environment DaemonEnvironment) error) Daemon {
	return FuncDaemon(func(ctx context.Context, environment DaemonEnvironment) error {
		t := time.NewTicker(interval)
		defer t.Stop()
		logger := internal.LoggerFromContext(ctx)
		for {
			if err := handler(ctx, environment); err != nil {
				logger.Println("attempt failed:", err)
			}
			select {
			case <-t.C:
			case <-ctx.Done():
				return nil
			}
		}
	})
}
