package v1

import (
	"context"
	"time"

	"github.com/reddec/git-pipe/core"
)

func New(retryDeployInterval, gracefulTimeout time.Duration) *Environment {
	launcher := NewLauncher(retryDeployInterval, gracefulTimeout)
	registry := NewRegistry()
	return &Environment{
		launcher: launcher,
		registry: registry,
	}
}

type Environment struct {
	launcher *Launcher
	registry core.Registry
}

func (env *Environment) Launcher() core.Launcher {
	return env.launcher
}

func (env *Environment) Registry() core.Registry {
	return env.registry
}

func (env *Environment) Run(ctx context.Context) {
	env.launcher.Run(ctx, env)
}
