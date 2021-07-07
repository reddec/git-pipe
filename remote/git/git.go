package git

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/remote"
)

const (
	defaultBranch = "master"
)

func New(u url.URL) remote.Source {
	branch := u.Fragment
	if branch == "" {
		branch = defaultBranch
	}
	return &Git{
		rawURL: u.String(),
		url:    u,
		branch: branch,
	}
}

func FromURL(rawURL string) (remote.Source, error) {
	if !strings.Contains(rawURL, "://") { // no proto - default ssh
		if !regexp.MustCompile(`.*?:\d+/.*`).MatchString(rawURL) { // no port
			rawURL = strings.ReplaceAll(rawURL, ":", ":22/")
		}
		rawURL = "ssh://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	return New(*u), nil
}

type Git struct {
	rawURL string
	url    url.URL
	branch string
}

func (gc *Git) Ref() url.URL {
	return gc.url
}

func (gc *Git) Poll(ctx context.Context, targetDir string) (changed bool, err error) {
	invoker := internal.In(targetDir)
	var fresh = !cloned(targetDir)

	if fresh {
		if err = gc.clone(ctx, invoker); err != nil {
			return
		}
	} else {
		if err = gc.fetch(ctx, invoker); err != nil {
			return
		}
	}

	prevHash, err := gc.commitHash(ctx, invoker)
	if err != nil {
		return
	}

	if err = gc.reset(ctx, invoker); err != nil {
		return
	}

	newHash, err := gc.commitHash(ctx, invoker)
	if err != nil {
		return
	}

	changed = fresh || prevHash != newHash

	if !changed {
		return
	}

	return
}

func (gc *Git) clone(ctx context.Context, invoker internal.At) error {
	err := invoker.Do(ctx, "git", "clone", "--depth", "1", gc.rawURL, "-b", gc.branch, ".").Exec()
	if err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func (gc *Git) fetch(ctx context.Context, invoker internal.At) error {
	err := invoker.Do(ctx, "git", "fetch", "-q", "origin", gc.branch).Exec()
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

func (gc *Git) reset(ctx context.Context, invoker internal.At) error {
	err := invoker.Do(ctx, "git", "reset", "-q", "--hard", "origin/"+gc.branch).Exec()
	if err != nil {
		return fmt.Errorf("get reset: %w", err)
	}
	return nil
}

func (gc *Git) commitHash(ctx context.Context, invoker internal.At) (string, error) {
	hash, err := invoker.Do(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("get commit hash: %w", err)
	}
	return hash, nil
}

func cloned(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}
