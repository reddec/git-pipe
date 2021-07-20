package dckr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

var (
	errIPNotFound = errors.New("container IP not found")
)

func (dp *dockerPack) Start(ctx context.Context, env map[string]string) ([]packs.Service, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	err = cli.ContainerRemove(ctx, dp.projectName(), types.ContainerRemoveOptions{Force: true})
	if err != nil && !errIsNoContainer(err) {
		return nil, fmt.Errorf("remove old container: %w", err)
	}

	volumePaths, err := dp.declaredVolumes(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("list mount points: %w", err)
	}

	var mountPoints = make([]mount.Mount, 0, len(volumePaths))
	for _, cPath := range volumePaths {
		mountPoints = append(mountPoints, mount.Mount{
			Type:   mount.TypeVolume,
			Source: dp.projectName(), // volume created during Create step
			Target: cPath,
		})
	}

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: dp.imageID,
		Env:   toEnvList(env),
	}, &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name: "on-failure",
		},
		Mounts: mountPoints,
	}, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			dp.network.Name: {},
		},
	}, nil, dp.projectName())
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	err = cli.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	info, err := cli.ContainerInspect(ctx, res.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %w", err)
	}

	return dp.exposedServices(info)
}

func (dp *dockerPack) exposedServices(info types.ContainerJSON) ([]packs.Service, error) {
	networkInfo, ok := info.NetworkSettings.Networks[dp.network.Name]
	if !ok {
		return nil, fmt.Errorf("network %s: %w", dp.network.Name, errIPNotFound)
	}
	ip := networkInfo.IPAddress

	rootDomain := internal.ToDomain(dp.directory)

	var endpoints = make([]packs.Service, 0, len(info.Config.ExposedPorts))
	name := filepath.Base(dp.directory)
	for port := range info.Config.ExposedPorts {
		srv := packs.Service{
			Name:      name,
			Group:     name,
			Domain:    port.Port() + "." + rootDomain,
			Addresses: []string{ip + ":" + port.Port()},
		}
		endpoints = append(endpoints, srv)
	}

	var rootEndpoint *packs.Service

	if len(endpoints) == 1 {
		rootEndpoint = &endpoints[0]
	} else if len(endpoints) > 1 {
		rootEndpoint = pickRootServiceByPort(endpoints)
	}

	if rootEndpoint != nil {
		endpoints = append(endpoints, packs.Service{
			Name:      name,
			Group:     name,
			Domain:    rootDomain,
			Addresses: rootEndpoint.Addresses,
		})
	}

	return endpoints, nil
}

func errIsNoContainer(err error) bool {
	return err != nil && strings.Contains(err.Error(), "No such container")
}

func pickRootServiceByPort(endpoints []packs.Service) *packs.Service {
	priority := portsPriority()

	var (
		bestPriority = 999
		bestService  = -1
	)

	for i, srv := range endpoints {
		for _, addr := range srv.Addresses {
			_, port, _ := net.SplitHostPort(addr)
			p, ok := priority[port]
			if ok && p < bestPriority {
				bestPriority = p
				bestService = i
			}
		}
	}
	if bestService != -1 {
		return &endpoints[bestService]
	}

	if len(endpoints) > 0 { // just first in order
		return &endpoints[0]
	}
	return nil
}

func portsPriority() map[string]int {
	// small value means higher priority
	// nolint:gomnd
	return map[string]int{
		"80":   1,
		"8080": 2,
	}
}

func toEnvList(env map[string]string) []string {
	var ans = make([]string, 0, len(env))
	for k, v := range env {
		ans = append(ans, k+"="+v)
	}
	return ans
}
