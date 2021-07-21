package pipe_test

import (
	"context"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/core"
	v1 "github.com/reddec/git-pipe/core/v1"
	"github.com/reddec/git-pipe/cryptor/noecnryption"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepo(t *testing.T) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	env, err := v1.New(ctx, v1.DefaultConfig(), &nobackup.NoBackup{}, &noecnryption.NoEncryption{})
	require.NoError(t, err)
	defer env.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.Run(ctx)
	}()

	allEvents, err := env.Launcher().Subscribe(ctx, 100, true)
	require.NoError(t, err)
	core.LogEvents(internal.LoggerFromContext(ctx), allEvents)

	defer func() { <-done }()
	defer cancel()

	url := createRepo(ctx, "example-srv", map[string]string{
		"Dockerfile": `
FROM hashicorp/http-echo
EXPOSE 80
CMD -listen :80 -text "hello"
`,
	})

	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src, err := git.FromURL(url)
	require.NoError(t, err)

	events, err := env.Launcher().Subscribe(ctx, 100, false)
	require.NoError(t, err)
	defer env.Launcher().Unsubscribe(ctx, events)

	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name:   "@repo-monitor",
		Daemon: pipe.Repo(src, time.Second, false, tmpDir, nil),
	})
	require.NoError(t, err)

	timedCtx, timedCancel := context.WithTimeout(ctx, 10*time.Second)
	defer timedCancel()

	ready := core.WaitForLauncherEventContext(timedCtx, events, "example-srv", core.LauncherEventReady)
	assert.True(t, ready)

	srv, err := env.Registry().Find("example-srv", "")
	require.NoError(t, err)

	address, err := env.Network().Resolve(ctx, srv.Address)
	require.NoError(t, err)

	t.Log(address)
}

func createRepo(ctx context.Context, name string, files map[string]string) string {
	gitDir, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}

	repoDir, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}

	inBareRepo := internal.At(repoDir)

	// create git repo
	err = inBareRepo.Do(ctx, "git", "init", "--bare", name+".git").Exec()
	if err != nil {
		panic(err)
	}

	gitURL := "file://" + filepath.Join(repoDir, name+".git")

	inRepo := internal.At(gitDir)
	// clone repo and add docker-compose file
	err = inRepo.Do(ctx, "git", "clone", gitURL, ".").Exec()
	if err != nil {
		panic(err)
	}
	for fileName, content := range files {
		err := ioutil.WriteFile(filepath.Join(gitDir, fileName), []byte(content), 0755)
		if err != nil {
			panic(err)
		}
	}

	err = inRepo.Do(ctx, "git", "add", "-A").Exec()
	if err != nil {
		panic(err)
	}
	err = inRepo.Do(ctx, "git", "commit", "-m", "initial").Exec()
	if err != nil {
		panic(err)
	}
	err = inRepo.Do(ctx, "git", "push", "origin", "master").Exec()
	if err != nil {
		panic(err)
	}
	return gitURL
}
