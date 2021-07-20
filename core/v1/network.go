package v1

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func NewDockerNetwork(ctx context.Context, cli *client.Client, name string) (*DockerNetwork, error) {
	dn := &DockerNetwork{
		cli: cli,
	}

	if err := dn.init(ctx, name); err != nil {
		_ = cli.Close()
		return nil, err
	}

	return dn, nil
}

type DockerNetwork struct {
	cli *client.Client
	id  string
}

func (dn *DockerNetwork) ID() string {
	return dn.id
}

func (dn *DockerNetwork) Join(ctx context.Context, containerID string) (ip string, err error) {
	ip, err = dn.getIP(ctx, containerID)
	if err == nil {
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	err = dn.cli.NetworkConnect(ctx, dn.id, containerID, &network.EndpointSettings{})
	if err != nil {
		return "", fmt.Errorf("connect container %s to network %s: %w", containerID, dn.id, err)
	}
	return dn.getIP(ctx, containerID)
}

func (dn *DockerNetwork) getIP(ctx context.Context, containerID string) (ip string, err error) {
	info, err := dn.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}
	for _, netInfo := range info.NetworkSettings.Networks {
		if netInfo.NetworkID == dn.id {
			return netInfo.IPAddress, nil
		}
	}
	return "", os.ErrNotExist
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

	return nil
}
