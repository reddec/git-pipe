package dckr

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"

	"github.com/docker/docker/client"
)

func New(directory string, network packs.Network) packs.Pack {
	return &dockerPack{
		directory: directory,
		network:   network,
	}
}

type dockerPack struct {
	directory string
	network   packs.Network
	imageID   string
}

func (dp *dockerPack) String() string {
	return "docker"
}

func (dp *dockerPack) Build(ctx context.Context, env map[string]string) error {
	value, err := internal.In(dp.directory).Do(ctx, "docker", "build", "-q", ".").Env(env).Output()
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}
	dp.imageID = value

	err = internal.CreateVolume(ctx, dp.projectName())
	if err != nil {
		return fmt.Errorf("create volume: %w", err)
	}

	return nil
}

func (dp *dockerPack) Stop(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	err = cli.ContainerStop(ctx, dp.projectName(), nil)
	if err != nil && !errIsNoContainer(err) {
		return fmt.Errorf("stop container: %w", err)
	}

	return nil
}

func (dp *dockerPack) Volumes(ctx context.Context) ([]string, error) {
	return []string{dp.projectName()}, nil
}

func (dp *dockerPack) declaredVolumes(ctx context.Context, cli client.APIClient) ([]string, error) {
	info, _, err := cli.ImageInspectWithRaw(ctx, dp.imageID)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}

	var ans = make([]string, 0, len(info.Config.Volumes))
	for containerPath := range info.Config.Volumes {
		ans = append(ans, containerPath)
	}

	return ans, nil
}

func (dp *dockerPack) Name() string {
	return filepath.Base(dp.directory)
}

func (dp *dockerPack) projectName() string {
	return packs.Namespace + "-" + filepath.Base(dp.directory)
}
