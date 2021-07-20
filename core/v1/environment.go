package v1

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/cryptor"
)

func New(ctx context.Context, config Config, provider backup.Backup, encryption cryptor.Cryptor) (*Environment, error) {
	network, err := NewDockerNetwork(ctx, config.NetworkName)
	if err != nil {
		return nil, fmt.Errorf("initialize networking: %w", err)
	}
	storage, err := NewVolumeStorage(provider, encryption, config.TempDir, config.Driver)
	if err != nil {
		return nil, fmt.Errorf("initialize storage: %w", err)
	}
	launcher := NewLauncher(config.RetryDeployInterval, config.GracefulTimeout)
	registry := NewRegistry()
	return &Environment{
		launcher: launcher,
		network:  network,
		registry: registry,
		storage:  storage,
	}, nil
}

type Environment struct {
	launcher *Launcher
	network  *DockerNetwork
	registry core.Registry
	storage  *VolumeStorage
}

func (env *Environment) Launcher() core.Launcher {
	return env.launcher
}

func (env *Environment) Registry() core.Registry {
	return env.registry
}

func (env *Environment) Storage() core.Storage {
	return env.storage
}

func (env *Environment) Network() core.Network {
	return env.network
}

func (env *Environment) Run(ctx context.Context) {
	env.launcher.Run(ctx, env)
}

func (env *Environment) Close() error {
	var all *multierror.Error
	if err := env.storage.Close(); err != nil {
		all = multierror.Append(all, err)
	}
	if err := env.network.Close(); err != nil {
		all = multierror.Append(all, err)
	}
	return all
}
