package v1_test

import (
	"context"
	"testing"

	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/core"
	v1 "github.com/reddec/git-pipe/core/v1"
	"github.com/reddec/git-pipe/cryptor/noecnryption"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLauncher_Launch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	env, err := v1.New(ctx, v1.DefaultConfig(), &nobackup.NoBackup{}, &noecnryption.NoEncryption{})
	require.NoError(t, err)
	defer env.Close()

	go func() {
		env.Run(ctx)
		close(done)
	}()

	ch, err := env.Launcher().Subscribe(ctx, 100, false)
	require.NoError(t, err)

	var run bool
	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name: "test",
		Daemon: core.FuncDaemon(func(ctx context.Context, environment core.DaemonEnvironment) error {
			run = true
			environment.Ready()
			return nil
		}),
	})
	require.NoError(t, err)

	started := core.WaitForLauncherEvent(ch, "test", core.LauncherEventReady)
	assert.True(t, started)

	err = env.Launcher().Remove(ctx, "test")
	require.NoError(t, err)

	assert.True(t, run)

	stopped := core.WaitForLauncherEvent(ch, "test", core.LauncherEventStopped)
	assert.True(t, stopped)

	err = env.Launcher().Unsubscribe(ctx, ch)
	require.NoError(t, err)

	cancel()
	<-done
}
