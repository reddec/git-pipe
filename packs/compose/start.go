package compose

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"

	"github.com/docker/docker/client"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"
)

func (dc *dockerCompose) Start(ctx context.Context, env map[string]string) ([]packs.Service, error) {
	mocked, err := dc.mockManifest()
	if err != nil {
		return nil, fmt.Errorf("mock: %w", err)
	}
	tmpFile, err := ioutil.TempFile(dc.directory, ".*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create temp docker-compose: %w", err)
	}

	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(mocked.Config); err != nil {
		return nil, fmt.Errorf("write temp docker-compose: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp docker-compose: %w", err)
	}

	if err := dc.at().Do(ctx, "docker-compose", "-p", dc.projectName(), "-f", filepath.Base(tmpFile.Name()), "up", "-d", "--remove-orphans").Env(env).Exec(); err != nil {
		return nil, fmt.Errorf("docker-compose up: %w", err)
	}

	return dc.exposedPorts(ctx, mocked)
}

type servicePort struct {
	Name          string
	Domain        string
	ContainerPort string
	Scale         int
}

type manifest struct {
	Services []servicePort
	Root     *servicePort
	Domain   string
	Config   string
}

func (cc *composeConfig) Load(dir string) error {
	rawConfig, err := readFirst(dir, "docker-compose.yaml", "docker-compose.yml")
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal([]byte(rawConfig), cc); err != nil {
		return fmt.Errorf("parse config; %w", err)
	}
	return nil
}

func (dc *dockerCompose) mockManifest() (*manifest, error) {
	var config composeConfig

	if err := config.Load(dc.directory); err != nil {
		return nil, fmt.Errorf("config; %w", err)
	}

	var domain = config.Domain
	if domain == "" {
		domain = internal.ToDomain(dc.directory)
	}

	var services = make(map[string]servicePort)
	var root *servicePort

	endpoints := make([]servicePort, 0)
	for name, service := range config.Services {
		serviceEndpoints := dc.mapService(name, service)
		// we remembered ports so we can now remove them. They will be expose over router.
		service.Ports = nil

		if len(serviceEndpoints) > 0 {
			// setup root service as link to first port
			if service.Root {
				root = &serviceEndpoints[0]
			}
			// setup default service port
			services[serviceEndpoints[0].Name] = serviceEndpoints[0]
		}
		if len(service.Ports) > 0 {
			// attach to default and to routing network
			service.Networks = append(service.Networks, "default", dc.network.Name)
		}
		endpoints = append(endpoints, serviceEndpoints...)
	}

	if config.Networks == nil {
		config.Networks = map[string]network{}
	}

	config.Networks[dc.network.Name] = network{
		External: true,
	}

	if root == nil {
		root = pickRootByPriority(services)
	}

	newConfig, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal new config: %w", err)
	}

	return &manifest{
		Domain:   domain,
		Services: endpoints,
		Root:     root,
		Config:   string(newConfig),
	}, nil
}

func (dc *dockerCompose) exposedPorts(ctx context.Context, config *manifest) ([]packs.Service, error) {
	indexByName := map[string][]servicePort{}
	for _, srv := range config.Services {
		indexByName[srv.Name] = append(indexByName[srv.Name], srv)
	}
	var group = filepath.Base(dc.directory)

	var services = map[string]*packs.Service{}

	// detect IP
	serviceIPs, err := dc.getIPS(ctx)
	if err != nil {
		return nil, fmt.Errorf("get ips: %w", err)
	}

	for _, serviceInfo := range config.Services {
		addrs, ok := serviceIPs[serviceInfo.Name]
		if !ok {
			continue
		}

		service, ok := services[serviceInfo.Domain]
		if !ok {
			service = &packs.Service{
				Group:  group,
				Name:   serviceInfo.Name,
				Domain: serviceInfo.Domain + "." + config.Domain,
			}
			services[serviceInfo.Domain] = service
		}

		for _, addr := range addrs {
			service.Addresses = append(service.Addresses, addr+":"+serviceInfo.ContainerPort)
		}
	}
	// detect root
	if config.Root != nil {
		for _, srv := range config.Services {
			if srv.Domain == config.Root.Domain {
				services[config.Domain] = &packs.Service{
					Group:     group,
					Name:      srv.Name,
					Domain:    config.Domain,
					Addresses: services[srv.Name].Addresses,
				}

				break
			}
		}
	}
	var ans = make([]packs.Service, 0, len(services))
	// flat to slice
	for _, srv := range services {
		if len(srv.Addresses) > 0 {
			ans = append(ans, *srv)
		}
	}

	return ans, nil
}

func (dc *dockerCompose) mapService(name string, service *service) (endpoints []servicePort) {
	var domain = service.Domain
	if domain == "" {
		domain = name
	}
	for i, port := range service.Ports {
		parts := strings.Split(port, ":")
		containerPort := parts[len(parts)-1]
		if containerPort == "" {
			continue
		}
		var scale = service.Scale
		if scale == 0 {
			scale = 1
		}
		portDomain := domain
		if i > 0 {
			portDomain = containerPort + "." + portDomain
		}
		sp := servicePort{
			Name:          name,
			Domain:        portDomain,
			ContainerPort: containerPort,
			Scale:         scale,
		}
		endpoints = append(endpoints, sp)
	}
	if len(service.Ports) > 0 {
		service.Networks = append(service.Networks, "default", dc.network.Name)
	}

	return
}

func readFirst(dir string, files ...string) (string, error) {
	var errs []error
	for _, file := range files {
		data, err := ioutil.ReadFile(filepath.Join(dir, file))
		if err != nil {
			errs = append(errs, err)

			continue
		}

		return string(data), nil
	}

	if err := (&multierror.Error{Errors: errs}).ErrorOrNil(); err != nil {
		return "", fmt.Errorf("read first: %w", err)
	}

	return "", os.ErrNotExist
}

func (dc *dockerCompose) getIPS(ctx context.Context) (map[string][]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	defer cli.Close()

	ids, err := dc.at().Do(ctx, "docker-compose", "-p", dc.projectName(), "ps", "-q", "-a").Output()
	if err != nil {
		return nil, fmt.Errorf("list all containers: %w", err)
	}

	var ips = make(map[string][]string)

	for _, id := range strings.Split(ids, "\n") {
		info, err := cli.ContainerInspect(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", id, err)
		}

		networkInfo, ok := info.NetworkSettings.Networks[dc.network.Name]
		if !ok {
			continue
		}
		var ipAddress = networkInfo.IPAddress
		serviceName := info.Config.Labels["com.docker.compose.service"]
		ips[serviceName] = append(ips[serviceName], ipAddress)
	}

	return ips, nil
}

func (dc *dockerCompose) at() internal.At {
	return internal.In(dc.directory)
}

func pickRootByPriority(services map[string]servicePort) *servicePort {
	// no defined x-root - pick by name and priority
	for _, name := range rootPriority() {
		sp, ok := services[name]
		if ok {
			return &sp
		}
	}
	return nil
}

func rootPriority() []string {
	return []string{"www", "web", "gateway"}
}
