package compose

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/reddec/git-pipe/packs"
)

func New(directory string, network packs.Network) packs.Pack {
	return &dockerCompose{
		directory: directory,
		network:   network,
	}
}

type dockerCompose struct {
	directory string
	network   packs.Network
}

func (dc *dockerCompose) String() string {
	return "docker-compose"
}

func (dc *dockerCompose) projectName() string {
	return packs.Namespace + "-" + filepath.Base(dc.directory)
}

func (dc *dockerCompose) Build(ctx context.Context, env map[string]string) error {
	err := dc.at().Do(ctx, "docker-compose", "-p", dc.projectName(), "build", "--pull", "--force-rm").Env(env).Exec()
	if err != nil {
		return fmt.Errorf("docker-compose build: %w", err)
	}

	return nil
}

func (dc *dockerCompose) Stop(ctx context.Context) error {
	err := dc.at().Do(ctx, "docker-compose", "-p", dc.projectName(), "stop").Exec()
	if err != nil {
		return fmt.Errorf("docker-compose stop: %w", err)
	}

	return nil
}

func (dc *dockerCompose) Volumes(ctx context.Context) ([]string, error) {
	var config composeConfig

	if err := config.Load(dc.directory); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var ans = make([]string, 0, len(config.Volumes))
	for name, decl := range config.Volumes {
		if decl.External || !(decl.Driver == "local" || decl.Driver == "") {
			continue
		}

		ans = append(ans, dc.projectName()+"_"+name)
	}

	return ans, nil
}
