package pipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/packs/dckr"
	"github.com/reddec/git-pipe/remote"
)

const DefaultBuffer = 1024

func Repo(source remote.Source, poll time.Duration, fqdn bool, workDir string, env map[string]string) core.Daemon {
	var name string
	if fqdn {
		name = generateFullName(source.Ref())
	} else {
		name = generateSimpleName(source.Ref())
	}
	var (
		baseDir = filepath.Join(workDir, name)
		force   = true
	)
	return core.FuncTimer(poll, func(ctx context.Context, environment core.DaemonEnvironment) error {
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			return fmt.Errorf("create base dir: %w", err)
		}
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
		var daemon core.Daemon
		switch {
		case hasAnyFile(baseDir, "Dockerfile"):
			daemon = dckr.New(baseDir, env)
		default:
			return errUnknownPackage
		}

		if err := environment.Global().Launcher().Launch(ctx, core.Descriptor{
			Name:   name,
			Daemon: daemon,
		}); err != nil {
			return fmt.Errorf("launch new version: %w", err)
		}

		core.WaitForLauncherEvent(updates, name, core.LauncherEventScheduled)

		environment.Ready()
		force = false
		return nil
	})
}
