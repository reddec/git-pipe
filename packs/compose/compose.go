package compose

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/loader"
	"github.com/compose-spec/compose-go/types"
	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
	"gopkg.in/yaml.v2"
)

func Run(ctx context.Context, env *core.Environment) error {
	rootDir, err := filepath.Abs(env.Directory)
	if err != nil {
		return fmt.Errorf("detect root path of workdir: %w", err)
	}
	at := internal.At(env.Directory)

	// Read environment file if possible
	environ, err := internal.ReadEnvFile(filepath.Join(rootDir, ".env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read env file: %w", err)
	}

	// Merge environment, where env from core has higher priority
	merged := make(map[string]string)
	for k, v := range environ {
		merged[k] = v
	}
	for k, v := range env.Vars {
		merged[k] = v
	}
	env.Vars = merged

	// Read first compose file
	fileName, content, err := readComposeFile(env.Directory, "docker-compose.yaml", "docker-compose.yml")
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	// Parse config
	project, err := loader.Load(types.ConfigDetails{
		Environment: env.Vars,
		ConfigFiles: []types.ConfigFile{{
			Filename: fileName,
			Content:  content,
		}},
	})
	if err != nil {
		return fmt.Errorf("load compose config: %w", err)
	}

	// Remove published ports
	for i, srv := range project.Services {
		for j, port := range srv.Ports {
			port.Published = 0
			srv.Ports[j] = port
		}
		project.Services[i] = srv
	}

	// Get volumes and apply workarounds
	var volumes []string
	for name, volume := range project.Volumes {
		if !volume.External.External && (volume.Driver == "" || volume.Driver == "local") {
			if strings.HasPrefix(volume.Name, "_") {
				// Workaround to provide valid volume name
				volume.Name = env.Name + volume.Name
				project.Volumes[name] = volume
			}
			volumes = append(volumes, volume.Name)
		}
	}

	// Apply service bind workaround
	for name, service := range project.Services {
		for i, volume := range service.Volumes {
			if volume.Type == types.VolumeTypeBind {
				if volume.Bind != nil {
					// remove unsupported features
					volume.Bind.CreateHostPath = false
				}
				if !filepath.IsAbs(volume.Source) {
					volume.Source = filepath.Join(rootDir, volume.Source)
				}
			}
			service.Volumes[i] = volume
		}
		project.Services[name] = service
	}

	// Serialize updated config
	composeContent, err := yaml.Marshal(project)
	if err != nil {
		return fmt.Errorf("marshall compose config: %w", err)
	}

	// Recover volumes (if applicable)
	err = env.Backup.Restore(ctx, env.Name, volumes)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	// Schedule backup
	backupTask := env.Backup.Schedule(ctx, env.Name, volumes)
	defer backupTask.Stop()

	// Build
	err = at.Do(ctx, "docker-compose", "-f", "-", "-p", env.Name, "build", "--pull", "--force-rm").Env(env.Vars).Input(composeContent).Exec()
	if err != nil {
		return fmt.Errorf("build image: %w", err)
	}

	// Bring up
	err = at.Do(ctx, "docker-compose", "-f", "-", "-p", env.Name, "up", "-d", "--remove-orphans").Env(env.Vars).Input(composeContent).Exec()
	if err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	// Tear down automatically
	defer at.Do(context.Background(), "docker-compose", "-f", "-", "-p", env.Name, "stop").Env(env.Vars).Input(composeContent).Exec()

	// Get deployed containers
	containers, err := mapContainers(ctx, env.Docker, env.Name, project.Services)
	if err != nil {
		return fmt.Errorf("get deplyed containers: %w", err)
	}

	// Collect exposed domains
	var firstDomainByService = make(map[string]string) // service -> domain
	var exposedLinks = make(map[string][]string)       // domains -> addresses
	for _, serviceContainers := range containers {
		var ports []types.ServicePortConfig

		for _, port := range serviceContainers.Service.Ports {
			// Filter only tcp or empty protocol
			if port.Protocol != "tcp" && port.Protocol != "" {
				continue
			}
			ports = append(ports, port)
		}

		// Filter only exposed
		if len(ports) == 0 {
			continue
		}

		// Attach all containers in the service to our network
		var links []string
		for _, container := range serviceContainers.Containers {
			address, err := env.Network.Join(ctx, container.ID)
			if err != nil {
				return fmt.Errorf("join container: %w", err)
			}
			links = append(links, address)

			// Automatically detach container from network
			defer env.Network.Leave(ctx, container.ID)
		}

		// Allocate domains
		domain := domainName(env.Name, any(serviceContainers.Service.DomainName, serviceContainers.Service.Name))
		domainsByPort := make(map[int]string)
		for i, port := range ports {
			serviceDomain := domainName(domain, strconv.FormatUint(uint64(port.Target), 10))
			exposedLinks[serviceDomain] = mapLinks(links, port.Target)

			if i == 0 {
				firstDomainByService[serviceContainers.Service.Name] = serviceDomain
			}
			domainsByPort[int(port.Target)] = serviceDomain
		}

		// Try pick root domain for service by ports priority
		for _, port := range packs.PortsPriority() {
			if serviceDomain, ok := domainsByPort[port]; ok {
				exposedLinks[domain] = exposedLinks[serviceDomain]
				break
			}
		}

		// Pick first port as root domain in case ports priority didn't work
		if _, picked := exposedLinks[domain]; !picked {
			exposedLinks[domain] = exposedLinks[domainsByPort[int(ports[0].Target)]]
		}

		// Check that service marked as root
		if isRoot, ok := serviceContainers.Service.Extensions["x-root"].(bool); ok && isRoot {
			exposedLinks[env.Name] = mapLinks(links, ports[0].Target)
		}
	}

	// Pick root domain by name if not yet picked
	suggestedRootDomain := selectRootDomain(firstDomainByService)
	if _, picked := exposedLinks[env.Name]; !picked && suggestedRootDomain != "" {
		exposedLinks[env.Name] = exposedLinks[suggestedRootDomain]
	}

	// Register containers in ingress
	err = env.Ingress.Set(ctx, env.Name, exposedLinks)
	if err != nil {
		return fmt.Errorf("set ingress: %w", err)
	}
	defer env.Ingress.Clear(context.Background(), env.Name)

	// Register in DNS
	var domains []string
	for domain := range exposedLinks {
		domains = append(domains, domain)
	}
	err = env.DNS.Register(ctx, domains)
	if err != nil {
		return fmt.Errorf("register DNS records: %w", err)
	}

	// Notify that system is up
	env.Event.Ready()

	// Wait till the end
	<-ctx.Done()
	return nil
}

type serviceContainers struct {
	Service    types.ServiceConfig
	Containers []dockerTypes.Container
}

func mapLinks(links []string, port uint32) []string {
	var list = make([]string, 0, len(links))
	containerPort := strconv.FormatUint(uint64(port), 10)

	for _, link := range links {
		list = append(list, link+":"+containerPort)
	}
	return list
}

func mapContainers(ctx context.Context, cli client.APIClient, project string, services []types.ServiceConfig) (map[string]*serviceContainers, error) {
	const (
		labelProject = "com.docker.compose.project"
		labelName    = "com.docker.compose.service"
	)
	filter := filters.NewArgs(filters.Arg("label", labelProject+"="+project))

	list, err := cli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: filter,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var servicesByName = make(map[string]types.ServiceConfig)
	for _, srv := range services {
		servicesByName[srv.Name] = srv
	}

	var result = make(map[string]*serviceContainers)

	// Map containers (could be multiple) to the service
	for _, container := range list {
		serviceName := container.Labels[labelName]
		info, known := servicesByName[serviceName]
		if !known {
			continue
		}
		srv, ok := result[serviceName]
		if !ok {
			srv = &serviceContainers{
				Service: info,
			}
			result[serviceName] = srv
		}
		srv.Containers = append(srv.Containers, container)
	}

	return result, nil
}

func selectRootDomain(domainByService map[string]string) string {
	for _, name := range packs.NamePriority() {
		domain, ok := domainByService[name]
		if ok {
			return domain
		}
	}
	return ""
}

func any(options ...string) string {
	for _, opt := range options {
		if opt != "" {
			return opt
		}
	}
	return ""
}

func domainName(root string, name string) string {
	if name != "" {
		return name + "." + root
	}
	return root
}

func volumeName(root string, volume string) string {
	return root + "_" + volume
}

func readComposeFile(dir string, names ...string) (string, []byte, error) {
	for _, name := range names {
		data, err := ioutil.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return name, data, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("read compose file: %w", err)
		}
	}
	return "", nil, os.ErrNotExist
}
