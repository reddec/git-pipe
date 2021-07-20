package pipe

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/remote"
)

const DefaultBuffer = 1024

func Repo(source remote.Source, poll time.Duration, workDir string) core.Daemon {
	// TODO: calculate base name
	var (
		name    = source.Ref().Path
		baseDir = filepath.Join(workDir, name)
		force   = true
	)
	return core.FuncTimer(poll, func(ctx context.Context, environment core.DaemonEnvironment) error {
		changed, err := source.Poll(ctx, baseDir)
		if err != nil {
			return fmt.Errorf("poll source: %w", err)
		}
		if !changed && !force {
			return nil
		}
		force = true
		updates, err := environment.Global().Launcher().Subscribe(ctx, DefaultBuffer, false)
		if err != nil {
			return fmt.Errorf("subscribe for updates: %w", err)
		}
		defer environment.Global().Launcher().Unsubscribe(ctx, updates)

		if err := environment.Global().Launcher().Remove(ctx, name); err != nil {
			return fmt.Errorf("remove previous version: %w", err)
		}

		core.WaitForLauncherEvent(updates, name, core.LauncherEventRemoved|core.LauncherEventRemoveFailed)
		// TODO: detect pack
		if err := environment.Global().Launcher().Launch(ctx, core.Descriptor{
			Name:   name,
			Daemon: nil,
		}); err != nil {
			return fmt.Errorf("launch new version: %w", err)
		}

		core.WaitForLauncherEvent(updates, name, core.LauncherEventScheduled)

		environment.Ready()
		force = false
		return nil
	})
}
