package dckr

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
	"go.uber.org/zap"
)

func Run(ctx context.Context, env *core.Environment) error {
	logger := internal.SubLogger(ctx, "docker")
	ctx = internal.WithLogger(ctx, logger)

	// Remove old containers if possible
	logger.Debug("cleaning old containers")
	if err := cleanupContainers(ctx, env.Docker, env.Name); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	// Build image from source
	logger.Debug("building image")
	image, err := buildImage(ctx, env.Docker, env.Directory)
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	// Collect declared mount points
	var mountPoints = make([]string, 0, len(image.Config.Volumes))
	for containerPath := range image.Config.Volumes {
		mountPoints = append(mountPoints, containerPath)
	}

	// Collect declared ports
	var ports = make([]int, 0, len(image.Config.ExposedPorts))
	for port := range image.Config.ExposedPorts {
		ports = append(ports, port.Int())
	}

	// We are storing all mount points in a single volume with name equal to repo
	var volumes = []string{env.Name}

	// Restore content in volumes
	logger.Info("restoring volumes", zap.Strings("volumes", volumes))
	if err := env.Backup.Restore(ctx, env.Name, volumes); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	// Schedule backup
	logger.Debug("scheduling backup")
	var backup = env.Backup.Schedule(ctx, env.Name, volumes)
	defer backup.Stop()

	// Create container
	logger.Info("creating container")
	containerID, err := createContainer(ctx, env.Docker, image, volumes[0], env.Name, env.Vars)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}
	defer cleanupContainers(context.Background(), env.Docker, env.Name)

	// Attach container to network
	logger.Debug("joining network")
	link, err := env.Network.Join(ctx, containerID)
	if err != nil {
		return fmt.Errorf("join container to network: %w", err)
	}

	// Automatically detach network so we will clear resolve cache
	defer env.Network.Leave(context.Background(), containerID)

	// Launch container
	logger.Info("starting container")
	containerInfo, err := startContainer(ctx, env.Docker, containerID)
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}
	defer env.Docker.ContainerStop(context.Background(), containerID, nil)

	// Wait for healthy state if needed
	if containerInfo.State.Health != nil {
		logger.Info("wait for healthy status")
		if err := internal.WaitToBeHealthy(ctx, env.Docker, containerID, containerInfo.Created); err != nil {
			return fmt.Errorf("health checks: %w", err)
		}
	}

	// Register in the ingress
	addressesByDomains := exposedServices(image, env.Name, link)
	logger.Debug("register ingress", zap.Int("endpoints_num", len(addressesByDomains)))
	if err := env.Ingress.Set(ctx, env.Name, addressesByDomains); err != nil {
		return fmt.Errorf("set ingress: %w", err)
	}
	defer env.Ingress.Clear(context.Background(), env.Name)

	// Register DNS
	var domains = make([]string, 0, len(addressesByDomains))
	for domain := range addressesByDomains {
		domains = append(domains, domain)
	}
	logger.Debug("register DNS", zap.Strings("domains", domains))
	if err := env.DNS.Register(ctx, domains); err != nil {
		return fmt.Errorf("register DNS: %w", err)
	}

	// Notify that everything is ready
	logger.Info("ready")
	env.Event.Ready()

	// Wait
	<-ctx.Done()
	logger.Info("destroying")
	return nil
}

func startContainer(ctx context.Context, api client.APIClient, containerID string) (*types.ContainerJSON, error) {
	err := api.ContainerStart(ctx, containerID, types.ContainerStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	info, err := api.ContainerInspect(ctx, containerID)
	if err != nil {

		return nil, fmt.Errorf("inspect container: %w", err)
	}

	return &info, nil
}

func exposedServices(image types.ImageInspect, name string, link string) map[string][]string {
	addressesByDomain := make(map[string][]string)
	domainByPort := make(map[int]string)
	// general services mapped by port: <port>.<name>
	for port := range image.Config.ExposedPorts {
		if proto := port.Proto(); proto != "" && proto != "tcp" {
			continue
		}
		portValue := port.Port()
		domain := portValue + "." + name
		addressesByDomain[domain] = []string{link + ":" + portValue}
		domainByPort[port.Int()] = domain
	}

	// get root domain by port priority
	for _, port := range packs.PortsPriority() {
		if domain, ok := domainByPort[port]; ok {
			addressesByDomain[name] = addressesByDomain[domain]
			break
		}
	}

	// get any port as root if needed
	if _, picked := addressesByDomain[name]; !picked {
		for _, addresses := range addressesByDomain {
			addressesByDomain[name] = addresses
			break
		}
	}
	return addressesByDomain
}

func createContainer(ctx context.Context, cli client.APIClient, image types.ImageInspect, volumeName, label string, env map[string]string) (string, error) {
	var mountPoints = make([]mount.Mount, 0, len(image.Config.Volumes))
	for pathInContainer := range image.Config.Volumes {
		mountPoints = append(mountPoints, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: pathInContainer,
		})
	}

	res, err := cli.ContainerCreate(ctx, &container.Config{
		Image: image.ID,
		Env:   toEnvList(env),
		Labels: map[string]string{
			"managed-by": "git-pipe",
			"git-pipe":   label,
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

func buildImage(ctx context.Context, cli client.APIClient, directory string) (types.ImageInspect, error) {
	tar, err := archive.TarWithOptions(directory, &archive.TarOptions{})
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
		logger.Debug(scanner.Text())

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
	logger.Info("image built", zap.String("image", lastID))
	return info, nil
}

func cleanupContainers(ctx context.Context, cli client.APIClient, label string) error {
	list, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "managed-by=git-pipe"), filters.Arg("label", "git-pipe="+label)),
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

func toEnvList(env map[string]string) []string {
	var ans = make([]string, 0, len(env))
	for k, v := range env {
		ans = append(ans, k+"="+v)
	}
	return ans
}
