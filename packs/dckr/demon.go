package dckr

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/hashicorp/go-multierror"
	"github.com/reddec/git-pipe/core"
)

type dockerDemon struct {
	env       map[string]string
	directory string

	imageID     string
	containerID string
	volumes     []string
	ip          string
}

func (dd *dockerDemon) Create(ctx context.Context, environment core.DaemonEnvironment) error {
	if err := dd.cleanupContainers(ctx, environment.Global().Docker(), environment.Name()); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	tar, err := archive.TarWithOptions(dd.directory, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("create tar from source dir: %w", err)
	}

	defer tar.Close()

	resp, err := environment.Global().Docker().ImageBuild(ctx, tar, types.ImageBuildOptions{
		SuppressOutput: true,
	})
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	var lastID string
	for scanner.Scan() {
		var result struct {
			AUX struct {
				ID string `json:"ID"`
			} `json:"aux"`
		}

		if id := result.AUX.ID; id != "" {
			lastID = id
		}
	}

	dd.imageID = lastID

	volumes, err := dd.declaredVolumes(ctx, environment.Global().Docker())
	if err != nil {
		return fmt.Errorf("detect declared volumes: %w", err)
	}

	dd.volumes = volumes

	// we are storing all mount points in a single volume with name equal to daemon
	var volumeName = environment.Name()

	if err := environment.Global().Storage().Restore(ctx, environment.Name(), []string{environment.Name()}); err != nil {
		return fmt.Errorf("restore volumes: %w", err)
	}

	if err := dd.createContainer(ctx, environment.Global().Docker(), volumeName, environment.Name()); err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if ip, err := environment.Global().Network().Join(ctx, dd.containerID); err != nil {
		return fmt.Errorf("join container to network: %w", err)
	} else {
		dd.ip = ip
	}

	return nil
}

func (dd *dockerDemon) Run(ctx context.Context, environment core.DaemonEnvironment) error {
	// TODO: implement lazy start here - check health status (https://docs.docker.com/engine/reference/builder/#healthcheck)
	err := environment.Global().Docker().ContainerStart(ctx, dd.containerID, types.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}
	<-ctx.Done()
	if err := environment.Global().Docker().ContainerStop(ctx, dd.containerID, nil); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}
	return nil
}

func (dd *dockerDemon) Remove(ctx context.Context, environment core.DaemonEnvironment) error {
	var all *multierror.Error
	if dd.containerID != "" {
		if err := environment.Global().Docker().ContainerStop(ctx, dd.containerID, nil); err != nil && !strings.Contains(err.Error(), "No such") {
			all = multierror.Append(all, fmt.Errorf("stop container: %w", err))
		}
	}
	if err := dd.cleanupContainers(ctx, environment.Global().Docker(), environment.Name()); err != nil {
		all = multierror.Append(fmt.Errorf("cleanup: %w", err))
	}
	return all
}

func (dd *dockerDemon) createContainer(ctx context.Context, cli client.APIClient, volumeName, daemonName string) error {
	var mountPoints = make([]mount.Mount, 0, len(dd.volumes))
	for _, cPath := range dd.volumes {
		mountPoints = append(mountPoints, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: cPath,
		})
	}

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: dd.imageID,
		Env:   toEnvList(dd.env),
		Labels: map[string]string{
			"managed-by": "git-pipe",
			"daemon":     daemonName,
		},
	}, &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name: "on-failure",
		},
		Mounts: mountPoints,
	}, &network.NetworkingConfig{}, nil, "")

	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	dd.containerID = res.ID

	return nil
}

func (dd *dockerDemon) cleanupContainers(ctx context.Context, cli client.APIClient, daemonName string) error {
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "managed-by=git-pipe"), filters.Arg("label", "daemon="+daemonName)),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	var all *multierror.Error
	for _, ct := range list {
		err = cli.ContainerRemove(ctx, ct.ID, types.ContainerRemoveOptions{
			Force: true,
		})
		if err != nil {
			all = multierror.Append(all, fmt.Errorf("remove container %s: %w", ct.ID, err))
		}
	}
	return all
}

func (dd *dockerDemon) declaredVolumes(ctx context.Context, cli client.APIClient) ([]string, error) {
	info, _, err := cli.ImageInspectWithRaw(ctx, dd.imageID)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}

	var ans = make([]string, 0, len(info.Config.Volumes))
	for containerPath := range info.Config.Volumes {
		ans = append(ans, containerPath)
	}

	return ans, nil
}

func (dd *dockerDemon) declaredPorts() {

}
