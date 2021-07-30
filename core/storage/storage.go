package storage

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/cryptor"
	"github.com/reddec/git-pipe/internal"
)

func Default(provider backup.Backup, cli *client.Client, encryption cryptor.Cryptor) *VolumeStorage {
	return New(provider, cli, encryption, "", "local", time.Hour)
}

func New(provider backup.Backup, cli *client.Client, encryption cryptor.Cryptor, tempDir string, driver string, interval time.Duration) *VolumeStorage {
	return &VolumeStorage{
		cli:        cli,
		provider:   provider,
		encryption: encryption,
		tempDir:    tempDir,
		driver:     driver,
		interval:   interval,
	}
}

type VolumeStorage struct {
	cli        *client.Client
	provider   backup.Backup
	encryption cryptor.Cryptor
	tempDir    string
	driver     string
	interval   time.Duration
}

func (sw *VolumeStorage) Restore(ctx context.Context, name string, volumeNames []string) error {
	if err := sw.ensureVolumes(ctx, volumeNames); err != nil {
		return fmt.Errorf("create volumes if needed: %w", err)
	}

	encryptedFile, err := ioutil.TempFile(sw.tempDir, "")
	if err != nil {
		return fmt.Errorf("create temp encrypted: %w", err)
	}
	if err := encryptedFile.Close(); err != nil {
		return fmt.Errorf("close encrypted file: %w", err)
	}
	defer os.RemoveAll(encryptedFile.Name())

	if err := sw.provider.Restore(ctx, name, encryptedFile.Name()); errors.Is(err, backup.ErrBackupNotExists) {
		return nil
	} else if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}

	rawFile, err := ioutil.TempFile(sw.tempDir, "")
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}

	if err := rawFile.Close(); err != nil {
		return fmt.Errorf("close raw file: %w", err)
	}
	defer os.RemoveAll(rawFile.Name())

	if err := sw.encryption.Decrypt(ctx, encryptedFile.Name(), rawFile.Name()); err != nil {
		return fmt.Errorf("decrypt archive: %w", err)
	}

	return sw.copyArchiveToVolumes(ctx, volumeNames, rawFile.Name())
}

func (sw *VolumeStorage) Backup(ctx context.Context, name string, volumeNames []string) error {
	rawFile, err := ioutil.TempFile(sw.tempDir, "")
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}

	if err := rawFile.Close(); err != nil {
		return fmt.Errorf("close raw file: %w", err)
	}

	defer os.RemoveAll(rawFile.Name())

	if err := sw.copyVolumesToArchive(ctx, volumeNames, rawFile.Name()); err != nil {
		return fmt.Errorf("copy volumes to archive: %w", err)
	}

	encryptedFile, err := ioutil.TempFile(sw.tempDir, "")
	if err != nil {
		return fmt.Errorf("create temp encrypted: %w", err)
	}
	if err := encryptedFile.Close(); err != nil {
		return fmt.Errorf("close encrypted file: %w", err)
	}
	defer os.RemoveAll(encryptedFile.Name())

	if err := sw.encryption.Encrypt(ctx, rawFile.Name(), encryptedFile.Name()); err != nil {
		return fmt.Errorf("encrypt archive file: %w", err)
	}

	if err := sw.provider.Backup(ctx, name, encryptedFile.Name()); err != nil {
		return fmt.Errorf("upload archive: %w", err)
	}
	return nil
}

func (sw *VolumeStorage) Schedule(ctx context.Context, name string, volumeNames []string) *internal.Task {
	return internal.Timer(ctx, sw.interval, func(ctx context.Context) error {
		return sw.Backup(ctx, name, volumeNames)
	})
}

func (sw *VolumeStorage) copyVolumesToArchive(ctx context.Context, volumeNames []string, targetFile string) error {
	var mounts = make([]mount.Mount, 0, len(volumeNames)+1)

	for _, volumeName := range volumeNames {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeVolume,
			Source:   volumeName,
			Target:   "/mnt/" + volumeName,
			ReadOnly: true,
		})
	}

	mounts = append(mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: targetFile,
		Target: "/backup.tar.gz",
	})

	res, err := sw.cli.ContainerCreate(ctx, &container.Config{
		Image: "busybox",
		Cmd:   []string{"tar", "-C", "/mnt", "--overwrite", "-zcf", "/backup.tar.gz", "."},
	}, &container.HostConfig{
		AutoRemove: true,
		Mounts:     mounts,
	}, &network.NetworkingConfig{}, nil, "")

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	err = sw.cli.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	ok, ec := sw.cli.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case res := <-ok:
		if res.Error != nil {
			return ErrDockerAPI(res.Error.Message)
		}

		return nil
	case err = <-ec:
		return err
	case <-ctx.Done():
		return ctx.Err() // nolint:wrapcheck
	}
}

func (sw *VolumeStorage) copyArchiveToVolumes(ctx context.Context, volumeNames []string, sourceFile string) error {
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
		Source:   sourceFile,
		Target:   "/backup.tar.gz",
		ReadOnly: true,
	})

	res, err := sw.cli.ContainerCreate(ctx, &container.Config{
		Image: "busybox",
		Cmd:   []string{"tar", "-C", "/mnt", "--overwrite", "-zxf", "/backup.tar.gz"},
	}, &container.HostConfig{
		AutoRemove: true,
		Mounts:     mounts,
	}, &network.NetworkingConfig{}, nil, "")

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	err = sw.cli.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})

	if err != nil {
		return fmt.Errorf("create backup container: %w", err)
	}

	ok, ec := sw.cli.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case res := <-ok:
		if res.Error != nil {
			return ErrDockerAPI(res.Error.Message)
		}

		return nil
	case err = <-ec:
		return err
	case <-ctx.Done():
		return ctx.Err() // nolint:wrapcheck
	}
}

func (sw *VolumeStorage) ensureVolumes(ctx context.Context, volumeNames []string) error {
	for _, name := range volumeNames {
		_, err := sw.cli.VolumeInspect(ctx, name)
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "No such") {
			return fmt.Errorf("inspect volume: %w", err)
		}

		_, err = sw.cli.VolumeCreate(ctx, volume.VolumeCreateBody{
			Driver: sw.driver,
			Name:   name,
		})
		if err != nil {
			return fmt.Errorf("create volume: %w", err)
		}
	}
	return nil
}

type ErrDockerAPI string

func (eap ErrDockerAPI) Error() string {
	return string(eap)
}
