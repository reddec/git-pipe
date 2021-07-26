package pipe

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs/dckr"
	"github.com/reddec/git-pipe/remote"
)

const DefaultBuffer = 1024

var (
	errUnknownPackage = errors.New("unknown packaging for repo")
)

func Default(source remote.Source, workDir string) core.Daemon {
	return Poller(source, time.Minute, time.Hour, false, workDir, nil)
}

func Poller(source remote.Source, poll, backup time.Duration, fqdn bool, workDir string, env map[string]string) core.Daemon {
	var name = Name(source.Ref(), fqdn)
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
			daemon = dckr.New(baseDir, env, backup)
		//case hasAnyFile(baseDir, "docker-compose.yaml", "docker-compose.yml"):
		//	daemon = compose.New(baseDir, pipe.network), nil
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

func Name(source url.URL, fqdn bool) string {
	var name string
	if fqdn {
		name = generateFullName(source)
	} else {
		name = generateSimpleName(source)
	}
	return name
}

func generateSimpleName(u url.URL) string {
	names := strings.Split(u.Path, "/")
	name := names[len(names)-1]
	name = strings.TrimSuffix(name, ".git")
	name = internal.ToDomain(name)
	if name == "" {
		return generateFullName(u)
	}
	return name
}

func generateFullName(u url.URL) string {
	baseName := u.Path

	name := strings.ReplaceAll(baseName, "/", ".")
	name = strings.Trim(name, ".git")
	name = internal.ToDomain(name)
	name = strings.Trim(name, ".")

	parts := strings.Split(name, ".")
	for i := 0; i < len(parts)/2; i++ {
		parts[i], parts[len(parts)-i-1] = parts[len(parts)-i-1], parts[i]
	}

	domain := strings.Join(parts, ".")

	if hostname := u.Hostname(); hostname != "" {
		domain += "." + hostname
	}
	return domain
}

func hasAnyFile(root string, files ...string) bool {
	for _, file := range files {
		if f, err := os.Stat(filepath.Join(root, file)); err == nil && !f.IsDir() {
			return true
		}
	}

	return false
}
