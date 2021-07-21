package dckr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	"github.com/reddec/git-pipe/internal"
)

func New(directory string, env map[string]string) core.Daemon {
	return &dockerDaemon{
		env:       env,
		directory: directory,
	}
}

type dockerDaemon struct {
	env       map[string]string
	directory string

	image       types.ImageInspect
	containerID string
	volumes     []string
	ports       []int
	address     string
	services    []core.Service
}

func (dd *dockerDaemon) Create(ctx context.Context, environment core.DaemonEnvironment) error {
	if err := dd.cleanupContainers(ctx, environment.Global().Docker(), environment.Name()); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	image, err := dd.buildImage(ctx, environment.Global().Docker())
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	dd.image = image
	dd.ports = dd.declaredPorts()
	dd.volumes = dd.declaredVolumes()

	// we are storing all mount points in a single volume with name equal to daemon
	var volumeName = environment.Name()

	if err := environment.Global().Storage().Restore(ctx, environment.Name(), []string{environment.Name()}); err != nil {
		return fmt.Errorf("restore volumes: %w", err)
	}

	containerID, err := dd.createContainer(ctx, environment.Global().Docker(), volumeName, environment.Name())
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}
	dd.containerID = containerID

	if address, err := environment.Global().Network().Join(ctx, dd.containerID); err != nil {
		return fmt.Errorf("join container to network: %w", err)
	} else {
		dd.address = address
	}

	dd.services = dd.exposedServices(environment.Name())
	return nil
}

func (dd *dockerDaemon) Run(ctx context.Context, environment core.DaemonEnvironment) error {
	// TODO: implement lazy start here - check health status (https://docs.docker.com/engine/reference/builder/#healthcheck)
	err := environment.Global().Docker().ContainerStart(ctx, dd.containerID, types.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	info, err := environment.Global().Docker().ContainerInspect(ctx, dd.containerID)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}

	if info.State.Health != nil {
		if err := internal.WaitToBeHealthy(ctx, environment.Global().Docker(), dd.containerID); err != nil {
			return fmt.Errorf("health checks: %w", err)
		}
	}

	for _, srv := range dd.services {
		if err := environment.Global().Registry().Register(srv); err != nil {
			return fmt.Errorf("register service %s: %w", srv.Label(), err)
		}
	}
	environment.Ready()

	<-ctx.Done()

	for _, srv := range dd.services {
		environment.Global().Registry().Unregister(srv.Namespace, srv.Name)
	}
	return nil
}

func (dd *dockerDaemon) Remove(ctx context.Context, environment core.DaemonEnvironment) error {
	for _, srv := range dd.services {
		environment.Global().Registry().Unregister(srv.Namespace, srv.Name)
	}

	var all *multierror.Error
	if dd.containerID != "" {
		if err := environment.Global().Docker().ContainerStop(ctx, dd.containerID, nil); err != nil && !strings.Contains(err.Error(), "No such") {
			all = multierror.Append(all, fmt.Errorf("stop container: %w", err))
		}
		if err := environment.Global().Network().Leave(ctx, dd.containerID); err != nil {
			all = multierror.Append(all, fmt.Errorf("unlink container: %w", err))
		}
	}
	if err := dd.cleanupContainers(ctx, environment.Global().Docker(), environment.Name()); err != nil {
		all = multierror.Append(fmt.Errorf("cleanup: %w", err))
	}
	return all.ErrorOrNil()
}

func (dd *dockerDaemon) exposedServices(namespace string) []core.Service {
	var services []core.Service
	// general services mapped by port: <port>.<name>
	for _, port := range dd.ports {
		services = append(services, core.Service{
			Namespace: namespace,
			Name:      strconv.Itoa(port),
			Address:   dd.address + ":" + strconv.Itoa(port),
		})
	}
	// mapping by priority
	if idx := findRootPort(dd.ports); idx != -1 {
		services = append(services, core.Service{
			Namespace: namespace,
			Address:   services[idx].Address,
		})
	}
	return services
}

func (dd *dockerDaemon) createContainer(ctx context.Context, cli client.APIClient, volumeName, daemonName string) (string, error) {
	var mountPoints = make([]mount.Mount, 0, len(dd.volumes))
	for _, cPath := range dd.volumes {
		mountPoints = append(mountPoints, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: cPath,
		})
	}

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: dd.image.ID,
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
		return "", fmt.Errorf("create container: %w", err)
	}

	return res.ID, nil
}

func (dd *dockerDaemon) buildImage(ctx context.Context, cli client.APIClient) (types.ImageInspect, error) {
	tar, err := archive.TarWithOptions(dd.directory, &archive.TarOptions{})
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("create tar from source dir: %w", err)
	}

	defer tar.Close()

	resp, err := cli.ImageBuild(ctx, tar, types.ImageBuildOptions{
		SuppressOutput: true,
	})
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("build image: %w", err)
	}

	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	logger := internal.LoggerFromContext(ctx)
	var lastID string
	for scanner.Scan() {
		logger.Println("build:", scanner.Text())

		var result struct {
			Stream string `json:"stream"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			return types.ImageInspect{}, fmt.Errorf("parse output: %w", err)
		}
		if id := strings.TrimSpace(result.Stream); id != "" {
			lastID = id
		}
	}

	info, _, err := cli.ImageInspectWithRaw(ctx, lastID)
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("inspect: %w", err)
	}

	return info, nil
}

func (dd *dockerDaemon) cleanupContainers(ctx context.Context, cli client.APIClient, daemonName string) error {
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
	return all.ErrorOrNil()
}

func (dd *dockerDaemon) declaredVolumes() []string {
	var ans = make([]string, 0, len(dd.image.Config.Volumes))
	for containerPath := range dd.image.Config.Volumes {
		ans = append(ans, containerPath)
	}
	return ans
}

func (dd *dockerDaemon) declaredPorts() []int {
	var ans []int

	for port := range dd.image.Config.ExposedPorts {
		ans = append(ans, port.Int())
	}
	return ans
}

func findRootPort(ports []int) int {
	if len(ports) == 0 {
		return -1
	}
	priority := portsPriority()

	var (
		bestPriority = 999
		bestService  = 0
	)

	for i, port := range ports {
		p, ok := priority[port]
		if ok && p < bestPriority {
			bestPriority = p
			bestService = i
		}
	}
	return bestService
}

func portsPriority() map[int]int {
	// small value means higher priority
	// nolint:gomnd
	return map[int]int{
		80:   1,
		8080: 2,
	}
}

func toEnvList(env map[string]string) []string {
	var ans = make([]string, 0, len(env))
	for k, v := range env {
		ans = append(ans, k+"="+v)
	}
	return ans
}
