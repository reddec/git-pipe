package pipe

import (
	"context"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/reddec/git-pipe/internal"
	git2 "github.com/reddec/git-pipe/remote/git"
	"github.com/reddec/git-pipe/router"

	"github.com/stretchr/testify/assert"
)

func TestPipe_Run(t *testing.T) {
	gitDir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(gitDir)

	repoDir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(repoDir)
	ctx := context.Background()

	inBareRepo := internal.At(repoDir)
	// create git repo
	err = inBareRepo.Do(ctx, "git", "init", "--bare", "my-project.git").Exec()
	assert.NoError(t, err)

	gitURL := "file://" + filepath.Join(repoDir, "my-project.git")

	inRepo := internal.At(gitDir)
	// clone repo and add docker-compose file
	err = inRepo.Do(ctx, "git", "clone", gitURL, ".").Exec()
	assert.NoError(t, err)

	composeManifest := `version: "3"
services:
  web:
    image: hashicorp/http-echo
    command: -listen :80 -text "web"
    ports:
    - 8080:80
    - 443:443
  srv:
    image: hashicorp/http-echo
    command: -listen :80 -text "srv"
    ports:
    - 8081:80

  env:
    image: ncarlier/webhookd
    entrypoint: /bin/sh
    command: "-c 'echo \"#!/bin/sh\" > /env.sh; echo env >> /env.sh; chmod +x /env.sh; exec webhookd --scripts /'"
    environment:
      TEST: "${MY_TEST}"
    ports:
    - 8080
`
	err = ioutil.WriteFile(filepath.Join(gitDir, "docker-compose.yaml"), []byte(composeManifest), 0600)
	if !assert.NoError(t, err) {
		return
	}

	err = inRepo.Do(ctx, "git", "add", "-A").Exec()
	assert.NoError(t, err)
	err = inRepo.Do(ctx, "git", "commit", "-m", "initial").Exec()
	assert.NoError(t, err)
	err = inRepo.Do(ctx, "git", "push", "origin", "master").Exec()
	assert.NoError(t, err)

	// create pipe
	workDir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(workDir)

	src, err := git2.FromURL(gitURL)
	assert.NoError(t, err)

	mgr, err := New(context.Background(), Config{
		Network:   "git-test-pipe",
		Directory: workDir,
		FQDN:      false,
		Poll:      time.Minute,
		Shutdown:  30 * time.Second,
		Domain:    "localhost",
	})
	assert.NoError(t, err)

	nx := router.New(router.Config{})
	nx.Handle(&router.Random{})
	srv := httptest.NewServer(nx)
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	mgr.Router(nx)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err = mgr.Run(ctx, src, map[string]string{
			"MY_TEST": "123",
		})
		if err != nil {
			assert.ErrorIs(t, err, context.Canceled)
		}
	}()

	<-mgr.Ready()
	u := "http://my-project.localhost:" + port
	t.Log(u)
	var ok bool
	for i := 0; i < 10; i++ {
		res, err := http.Get(u)
		if err != nil || res.StatusCode == http.StatusBadGateway {
			t.Log("attempt", i, "failed:", err)
			time.Sleep(time.Second)
			continue
		}
		ok = true
		break
	}

	assert.True(t, ok)
	res, err := http.Get("http://my-project.localhost:" + port)
	assert.NoError(t, err)
	data, err := ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "web\n", string(data))

	res, err = http.Get("http://web.my-project.localhost:" + port)
	assert.NoError(t, err)
	data, err = ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "web\n", string(data))

	res, err = http.Get("http://srv.my-project.localhost:" + port)
	assert.NoError(t, err)
	data, err = ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "srv\n", string(data))

	res, err = http.Post("http://env.my-project.localhost:"+port+"/env", "", nil)
	assert.NoError(t, err)
	data, err = ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, string(data), "123")

	cancel()
	<-done
}
