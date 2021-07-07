package internal

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

func CreateNetworkIfNeeded(ctx context.Context, name string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	info, err := cli.NetworkInspect(ctx, name, types.NetworkInspectOptions{})
	if err != nil && !strings.Contains(err.Error(), "No such") {
		return "", fmt.Errorf("inspect network: %w", err)
	} else if err == nil {
		return info.ID, nil
	}

	res, err := cli.NetworkCreate(ctx, name, types.NetworkCreate{
		CheckDuplicate: true,
	})

	if err != nil {
		return "", fmt.Errorf("create docker network: %w", err)
	}

	return res.ID, nil
}

func ArchiveVolume(ctx context.Context, volumeNames []string, targetFile string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	var mounts = make([]mount.Mount, 0, len(volumeNames)+1)

	for _, volume := range volumeNames {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeVolume,
			Source:   volume,
			Target:   "/mnt/" + volume,
			ReadOnly: true,
		})
	}

	mounts = append(mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: targetFile,
		Target: "/backup.tar.gz",
	})

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "busybox",
		Cmd:   []string{"tar", "-C", "/mnt", "--overwrite", "-zcf", "/backup.tar.gz", "."},
	}, &container.HostConfig{
		AutoRemove: true,
		Mounts:     mounts,
	}, &network.NetworkingConfig{}, nil, "")

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	err = cli.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	ok, ec := cli.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case res := <-ok:
		if res.Error != nil {
			return &ErrDockerAPI{Message: res.Error.Message}
		}

		return nil
	case err = <-ec:
		return err
	case <-ctx.Done():
		return ctx.Err() // nolint:wrapcheck
	}
}

func UnArchiveVolume(ctx context.Context, volumeNames []string, targetFile string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	var mounts = make([]mount.Mount, 0, len(volumeNames)+1)

	for _, volume := range volumeNames {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeVolume,
			Source:   volume,
			Target:   "/mnt/" + volume,
			ReadOnly: false,
		})
	}

	mounts = append(mounts, mount.Mount{
		Type:     mount.TypeBind,
		Source:   targetFile,
		Target:   "/backup.tar.gz",
		ReadOnly: true,
	})

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "busybox",
		Cmd:   []string{"tar", "-C", "/mnt", "--overwrite", "-zxf", "/backup.tar.gz"},
	}, &container.HostConfig{
		AutoRemove: true,
		Mounts:     mounts,
	}, &network.NetworkingConfig{}, nil, "")

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	err = cli.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	ok, ec := cli.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case res := <-ok:
		if res.Error != nil {
			return &ErrDockerAPI{Message: res.Error.Message}
		}

		return nil
	case err = <-ec:
		return err
	case <-ctx.Done():
		return ctx.Err() // nolint:wrapcheck
	}
}

func ContainerID() string {
	const path = `/proc/1/cpuset`
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println("failed detect container ID:", err)

		return ""
	}
	id := filepath.Base(strings.TrimSpace(string(data)))
	if id == "/" {
		return ""
	}

	return id
}

func JoinNetwork(ctx context.Context, containerID string, networkID string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	info, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}
	for _, netInfo := range info.NetworkSettings.Networks {
		if netInfo.NetworkID == networkID {
			return nil
		}
	}

	err = cli.NetworkConnect(ctx, networkID, containerID, &network.EndpointSettings{})
	if err != nil {
		return fmt.Errorf("connect container %s to network %s: %w", containerID, networkID, err)
	}

	return nil
}

func CreateVolume(ctx context.Context, name string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	_, err = cli.VolumeInspect(ctx, name)
	if err != nil && !strings.Contains(err.Error(), "No such") {
		return fmt.Errorf("inspect volume: %w", err)
	}
	if err == nil {
		return nil
	}

	_, err = cli.VolumeCreate(ctx, volume.VolumeCreateBody{
		Driver: "local",
		Name:   name,
	})
	if err != nil {
		return fmt.Errorf("create volume: %w", err)
	}

	return nil
}

type ErrDockerAPI struct {
	Message string
}

func (eda *ErrDockerAPI) Error() string {
	return eda.Message
}
