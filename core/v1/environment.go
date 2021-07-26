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
	reg := NewRegistry(config.Domain)
	return &Environment{
		launcher: launcher,
		network:  network,
		registry: reg,
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

type BackgroundEnvironment struct {
	done   chan struct{}
	finish func()
	core.Environment
}

func NewBackground(ctx context.Context, config Config, provider backup.Backup, encryption cryptor.Cryptor) (*BackgroundEnvironment, error) {
	env, err := New(ctx, config, provider, encryption)
	if err != nil {
		return nil, err
	}

	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer env.Close()
		defer cancel()

		env.Run(child)
	}()

	return &BackgroundEnvironment{
		done:        done,
		finish:      cancel,
		Environment: env,
	}, nil
}

func (be *BackgroundEnvironment) Wait() {
	<-be.done
}

func (be *BackgroundEnvironment) Stop() {
	be.finish()
	<-be.done
}
