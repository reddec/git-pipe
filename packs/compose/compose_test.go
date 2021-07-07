package compose_test

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
	"github.com/reddec/git-pipe/packs/compose"

	"github.com/stretchr/testify/assert"
)

func TestDockerCompose_Build(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(dir)

	yaml := `version: "3"
services:
  web:
    build: .
    ports:
    - 8080:80
`

	dockerfile := `FROM busybox
RUN date > time.txt`

	err = ioutil.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte(yaml), 0600)
	if !assert.NoError(t, err) {
		return
	}

	err = ioutil.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0600)
	if !assert.NoError(t, err) {
		return
	}

	dc := compose.New(dir, packs.Network{})

	err = dc.Build(context.Background(), nil)
	assert.NoError(t, err)
}

func TestDockerCompose_StartStop(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(dir)

	yaml := `version: "3"
x-domain: example.com
services:
  web:
    image: nginx
    ports:
    - 8080:80
    - 443:443
`

	err = ioutil.WriteFile(filepath.Join(dir, "docker-compose.yaml"), []byte(yaml), 0600)
	if !assert.NoError(t, err) {
		return
	}

	ctx := context.Background()

	network, err := internal.CreateNetworkIfNeeded(ctx, "git-pipe-test")
	assert.NoError(t, err)

	dc := compose.New(dir, packs.Network{
		Name: "git-pipe-test",
		ID:   network,
	})

	services, err := dc.Start(ctx, nil)
	assert.NoError(t, err)
	assert.Len(t, services, 3)
	t.Logf("%+v", services)
	root := findByDomain(services, "example.com")
	web := findByDomain(services, "web.example.com")
	ssl := findByDomain(services, "443.web.example.com")
	assert.NotEmpty(t, root)
	assert.NotEmpty(t, web)
	assert.NotEmpty(t, ssl)
	assert.Equal(t, web.Addresses, root.Addresses)
	assert.NotEqual(t, web, ssl)

	err = dc.Stop(ctx)
	assert.NoError(t, err)
}

func findByDomain(services []packs.Service, name string) packs.Service {
	for _, srv := range services {
		if srv.Domain == name {
			return srv
		}
	}
	return packs.Service{}
}
