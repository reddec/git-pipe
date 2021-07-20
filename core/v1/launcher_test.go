package v1_test

import (
	"context"
	"testing"
	"time"

	"github.com/reddec/git-pipe/core"
	v1 "github.com/reddec/git-pipe/core/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLauncher_Launch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	env := v1.New(time.Second, time.Second)
	go func() {
		env.Run(ctx)
		close(done)
	}()

	ch, err := env.Launcher().Subscribe(ctx, 100)
	require.NoError(t, err)

	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name: "test",
		Daemon: core.FuncDaemon(func(ctx context.Context, environment core.DaemonEnvironment) {

		}),
	})
	require.NoError(t, err)

	started := core.WaitForLauncherEvent(ch, "test", core.LauncherEventStarted)
	assert.True(t, started)

	err = env.Launcher().Remove(ctx, "test")
	require.NoError(t, err)

	stopped := core.WaitForLauncherEvent(ch, "test", core.LauncherEventStopped)
	assert.True(t, stopped)

	err = env.Launcher().Unsubscribe(ctx, ch)
	require.NoError(t, err)

	cancel()
	<-done
}
