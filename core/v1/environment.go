package v1

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/cryptor"
)

func New(ctx context.Context, config Config, provider backup.Backup, encryption cryptor.Cryptor) (*Environment, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	network, err := NewDockerNetwork(ctx, cli, config.NetworkName, config.DisableResolve)
	if err != nil {
		return nil, fmt.Errorf("initialize networking: %w", err)
	}

	storage := NewVolumeStorage(provider, cli, encryption, config.TempDir, config.Driver)
	launcher := NewLauncher(config.RetryDeployInterval, config.GracefulTimeout)
	registry := NewRegistry(config.Domain)
	return &Environment{
		launcher: launcher,
		network:  network,
		registry: registry,
		storage:  storage,
		client:   cli,
	}, nil
}

type Environment struct {
	launcher *Launcher
	network  *DockerNetwork
	registry core.Registry
	storage  *VolumeStorage
	client   *client.Client
}

func (env *Environment) Docker() client.APIClient {
	return env.client
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
	return env.client.Close()
}
