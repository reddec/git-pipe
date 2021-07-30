package pipe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs/compose"
	"github.com/reddec/git-pipe/packs/dckr"
	"github.com/reddec/git-pipe/remote"
	"go.uber.org/zap"
)

var (
	errUnknownPackage = errors.New("unknown packaging for repo")
)

func Run(ctx context.Context, source remote.Source, env *core.Environment, interval time.Duration) {
	logger := internal.SubLogger(ctx, env.Name)
	p := &poller{
		env:    env,
		logger: logger,
		source: source,
	}

	p.run(internal.WithLogger(ctx, logger), interval)
}

type poller struct {
	env     *core.Environment
	current *internal.Task
	logger  *zap.Logger
	source  remote.Source
}

func (poller *poller) run(ctx context.Context, interval time.Duration) {
	var force = true
	var updater = time.NewTicker(interval)
	defer updater.Stop()

LOOP:
	for {
		if err := poller.poll(ctx, force); err != nil {
			force = true
			poller.logger.Warn("poll failed", zap.Error(err))
		} else {
			force = false
		}

	STATE:
		for {
			select {
			case <-updater.C:
				break STATE
			case <-ctx.Done():
				break LOOP
			case <-poller.current.Wait():
				poller.logger.Warn("package stopped", zap.Error(poller.current.Error()))
				poller.current = nil
				force = true
			}
		}
	}

	if err := poller.current.Stop(); err != nil && !errors.Is(err, context.Canceled) {
		poller.logger.Warn("failed cleanup and stop package", zap.Error(err))
	}
}

func (poller *poller) poll(ctx context.Context, force bool) error {
	poller.logger.Debug("polling for updates")
	if err := os.MkdirAll(poller.env.Directory, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	changed, err := poller.source.Poll(ctx, poller.env.Directory)
	if err != nil {
		return fmt.Errorf("poll source: %w", err)
	}
	if !changed && !force {
		return nil
	}

	// stop old
	if err := poller.current.Stop(); err != nil {
		return fmt.Errorf("stop previous package: %w", err)
	}

	var task *internal.Task
	switch {
	case hasAnyFile(poller.env.Directory, "docker-compose.yaml", "docker-compose.yml"):
		poller.logger.Debug("package detected as docker-compose")
		task = internal.Spawn(ctx, func(ctx context.Context) error {
			return compose.Run(ctx, poller.env)
		})
	case hasAnyFile(poller.env.Directory, "Dockerfile"):
		poller.logger.Debug("package detected as Dockerfile")
		task = internal.Spawn(ctx, func(ctx context.Context) error {
			return dckr.Run(ctx, poller.env)
		})
	default:
		return errUnknownPackage
	}
	poller.current = task
	return nil
}

func hasAnyFile(root string, files ...string) bool {
	for _, file := range files {
		if f, err := os.Stat(filepath.Join(root, file)); err == nil && !f.IsDir() {
			return true
		}
	}

	return false
}
