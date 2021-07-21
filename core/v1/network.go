package v1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/reddec/git-pipe/internal"
)

var ErrNotAssigned = errors.New("alias not assigned for the container")

func NewDockerNetwork(ctx context.Context, cli *client.Client, name string) (*DockerNetwork, error) {
	dn := &DockerNetwork{
		cli:    cli,
		name:   name,
		selfID: internal.ContainerID(),
	}

	if err := dn.init(ctx, name); err != nil {
		_ = cli.Close()
		return nil, err
	}

	return dn, nil
}

type DockerNetwork struct {
	cli    *client.Client
	id     string
	name   string
	selfID string

	cache struct {
		lock    sync.RWMutex
		valid   bool
		network types.NetworkResource
	}
}

func (dn *DockerNetwork) ID() string {
	return dn.id
}

func (dn *DockerNetwork) Join(ctx context.Context, containerID string) (alias string, err error) {
	alias, err = dn.getAlias(ctx, containerID)
	if err == nil {
		return
	}
	if !errors.Is(err, ErrNotAssigned) {
		return "", err
	}

	err = dn.cli.NetworkConnect(ctx, dn.id, containerID, &network.EndpointSettings{})
	if err != nil {
		return "", fmt.Errorf("connect container %s to network %s: %w", containerID, dn.id, err)
	}
	dn.invalidateNetwork()
	return dn.getAlias(ctx, containerID)
}

func (dn *DockerNetwork) Leave(ctx context.Context, containerID string) error {
	err := dn.cli.NetworkDisconnect(ctx, dn.id, containerID, true)
	if err != nil {
		return fmt.Errorf("disconnect: %w", err)
	}
	dn.invalidateNetwork()
	return nil
}

func (dn *DockerNetwork) Resolve(ctx context.Context, address string) (string, error) {
	if dn.insideDocker() {
		return address, nil
	}

	info, err := dn.getNetwork(ctx)
	if err != nil {
		return "", fmt.Errorf("get network: %w", err)
	}

	var host string
	var port string
	if !strings.Contains(address, ":") {
		host = address
	} else {
		alias, p, err := net.SplitHostPort(address)
		if err != nil {
			return "", fmt.Errorf("split host port: %w", err)
		}
		host = alias
		port = p
	}

	for id, cont := range info.Containers {
		if cont.IPv4Address != "" && (strings.HasPrefix(id, host) || cont.Name == host) {
			ip := strings.SplitN(cont.IPv4Address, "/", 2)[0]
			if port != "" {
				return ip + ":" + port, nil
			}
			return ip, nil
		}
	}
	return "", ErrNotAssigned
}

func (dn *DockerNetwork) invalidateNetwork() {
	dn.cache.lock.Lock()
	defer dn.cache.lock.Unlock()
	dn.cache.valid = false
}

func (dn *DockerNetwork) getNetwork(ctx context.Context) (types.NetworkResource, error) {
	dn.cache.lock.RLock()
	if dn.cache.valid {
		dn.cache.lock.RUnlock()
		return dn.cache.network, nil
	}
	dn.cache.lock.RUnlock()

	dn.cache.lock.Lock()
	defer dn.cache.lock.Unlock()
	if dn.cache.valid {
		return dn.cache.network, nil
	}

	info, err := dn.cli.NetworkInspect(ctx, dn.id, types.NetworkInspectOptions{})
	if err != nil {
		return types.NetworkResource{}, fmt.Errorf("inspect network: %w", err)
	}
	dn.cache.valid = true
	dn.cache.network = info
	return info, nil
}

func (dn *DockerNetwork) getAlias(ctx context.Context, containerID string) (alias string, err error) {
	info, err := dn.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}
	for name, netInfo := range info.NetworkSettings.Networks {
		if name == dn.name {
			for _, alias := range netInfo.Aliases {
				return alias, nil
			}
		}
	}
	return "", ErrNotAssigned
}

func (dn *DockerNetwork) init(ctx context.Context, name string) error {
	info, err := dn.cli.NetworkInspect(ctx, name, types.NetworkInspectOptions{})
	if err != nil && !strings.Contains(err.Error(), "No such") {
		return fmt.Errorf("inspect network: %w", err)
	}
	if err == nil {
		dn.id = info.ID
		return nil
	}

	res, err := dn.cli.NetworkCreate(ctx, name, types.NetworkCreate{
		CheckDuplicate: true,
	})

	if err != nil {
		return fmt.Errorf("create docker network: %w", err)
	}
	dn.id = res.ID
	if !dn.insideDocker() {
		return nil
	}

	if _, err := dn.Join(ctx, dn.selfID); err != nil {
		return fmt.Errorf("join self to the network: %w", err)
	}
	return nil
}

func (dn *DockerNetwork) insideDocker() bool { return dn.selfID != "" }
