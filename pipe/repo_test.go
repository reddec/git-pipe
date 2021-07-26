package pipe_test

import (
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/core"
	v1 "github.com/reddec/git-pipe/core/v1"
	"github.com/reddec/git-pipe/cryptor/noecnryption"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote/git"
	"github.com/reddec/git-pipe/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRepo(t *testing.T) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	env, err := v1.New(ctx, v1.DefaultConfig(), &nobackup.NoBackup{}, &noecnryption.NoEncryption{})
	require.NoError(t, err)
	defer env.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.Run(ctx)
	}()

	allEvents, err := env.Launcher().Subscribe(ctx, 100, true)
	require.NoError(t, err)
	core.LogEvents(internal.LoggerFromContext(ctx), allEvents)

	defer func() { <-done }()
	defer cancel()

	url := createRepo(ctx, "example-srv", map[string]string{
		"Dockerfile": `
FROM hashicorp/http-echo
EXPOSE 80
HEALTHCHECK --interval=1s CMD ["/http-echo", "-version"]
CMD ["-listen", ":80", "-text", "hello"]
`,
	})

	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src, err := git.FromURL(url)
	require.NoError(t, err)

	events, err := env.Launcher().Subscribe(ctx, 100, false)
	require.NoError(t, err)
	defer env.Launcher().Unsubscribe(ctx, events)

	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name:   "@repo-monitor",
		Daemon: pipe.Default(src, tmpDir),
	})
	require.NoError(t, err)

	timedCtx, timedCancel := context.WithTimeout(ctx, 10*time.Second)
	defer timedCancel()

	ready := core.WaitForLauncherEventContext(timedCtx, events, "example-srv", core.LauncherEventReady)
	assert.True(t, ready)

	srv, err := env.Registry().Find("example-srv", "")
	require.NoError(t, err)

	address, err := env.Network().Resolve(ctx, srv.Address())
	require.NoError(t, err)

	t.Log(address)
}

func createRepo(ctx context.Context, name string, files map[string]string) string {
	gitDir, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}

	repoDir, err := ioutil.TempDir("", "")
	if err != nil {
		panic(err)
	}

	inBareRepo := internal.At(repoDir)

	// create git repo
	err = inBareRepo.Do(ctx, "git", "init", "--bare", name+".git").Exec()
	if err != nil {
		panic(err)
	}

	gitURL := "file://" + filepath.Join(repoDir, name+".git")

	inRepo := internal.At(gitDir)
	// clone repo and add docker-compose file
	err = inRepo.Do(ctx, "git", "clone", gitURL, ".").Exec()
	if err != nil {
		panic(err)
	}
	for fileName, content := range files {
		err := ioutil.WriteFile(filepath.Join(gitDir, fileName), []byte(content), 0755)
		if err != nil {
			panic(err)
		}
	}

	err = inRepo.Do(ctx, "git", "add", "-A").Exec()
	if err != nil {
		panic(err)
	}
	err = inRepo.Do(ctx, "git", "commit", "-m", "initial").Exec()
	if err != nil {
		panic(err)
	}
	err = inRepo.Do(ctx, "git", "push", "origin", "master").Exec()
	if err != nil {
		panic(err)
	}
	return gitURL
}

func TestRepo_Docker(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repoURL := createRepo(ctx, "my-project", map[string]string{
		"Dockerfile": `
FROM hashicorp/http-echo
CMD ["-text", "srv"]
`,
	})

	// create pipe
	workDir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(workDir)

	src, err := git.FromURL(repoURL)
	assert.NoError(t, err)
	cfg := v1.NewConfig(v1.Network("git-test-pipe"), v1.Domain("localhost"))

	env, err := v1.NewBackground(ctx, cfg, &nobackup.NoBackup{}, &noecnryption.NoEncryption{})
	require.NoError(t, err)
	defer env.Stop()
	defer cancel()

	go func() {
		for event := range env.Registry().Subscribe(1024, true) {
			zap.L().Debug("registry event",
				zap.String("service", event.Service.Name),
				zap.String("namespace", event.Service.Namespace),
				zap.String("domain", event.Service.Domain),
				zap.String("event", event.Event.String()))
		}
	}()

	allEvents, err := env.Launcher().Subscribe(ctx, 100, true)
	require.NoError(t, err)
	core.LogEvents(internal.LoggerFromContext(ctx), allEvents)

	const port = "29931"
	const bindAddr = "127.0.0.1:" + port

	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name: "@router",
		Daemon: router.New(router.Config{
			Bind: bindAddr,
		}),
	})
	require.NoError(t, err)

	err = env.Launcher().Launch(ctx, core.Descriptor{
		Name: "@poller/my-project",
		Daemon: pipe.Poller(src, time.Second, time.Minute, false, tmpDir, map[string]string{
			"MY_TEST": "123",
		}),
	})
	require.NoError(t, err)

	ready := core.WaitForEvent(ctx, env.Launcher(), "my-project", core.LauncherEventReady)
	require.True(t, ready)

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
	assert.Equal(t, "srv\n", string(data))
}

//
//func TestRepo_Compose(t *testing.T) {
//	logger, err := zap.NewDevelopment()
//	require.NoError(t, err)
//	zap.ReplaceGlobals(logger)
//	defer logger.Sync()
//
//	tmpDir, err := ioutil.TempDir("", "")
//	require.NoError(t, err)
//	defer os.RemoveAll(tmpDir)
//
//	gitDir, err := ioutil.TempDir("", "")
//	if !assert.NoError(t, err) {
//		return
//	}
//
//	defer os.RemoveAll(gitDir)
//
//	repoDir, err := ioutil.TempDir("", "")
//	if !assert.NoError(t, err) {
//		return
//	}
//
//	defer os.RemoveAll(repoDir)
//	ctx := context.Background()
//
//	repoURL := createRepo(ctx, "my-project", map[string]string{
//		"docker-compose.yaml": `version: "3"
//services:
//  web:
//    image: hashicorp/http-echo
//    command: -listen :80 -text "web"
//    ports:
//    - 8080:80
//    - 443:443
//  srv:
//    image: hashicorp/http-echo
//    command: -listen :80 -text "srv"
//    ports:
//    - 8081:80
//
//  env:
//    image: ncarlier/webhookd
//    entrypoint: /bin/sh
//    command: "-c 'echo \"#!/bin/sh\" > /env.sh; echo env >> /env.sh; chmod +x /env.sh; exec webhookd --scripts /'"
//    environment:
//      TEST: "${MY_TEST}"
//    ports:
//    - 8080
//`,
//	})
//
//	// create pipe
//	workDir, err := ioutil.TempDir("", "")
//	if !assert.NoError(t, err) {
//		return
//	}
//
//	defer os.RemoveAll(workDir)
//
//	src, err := git.FromURL(repoURL)
//	assert.NoError(t, err)
//	cfg := v1.NewConfig(v1.Network("git-test-pipe"), v1.NoResolve())
//	env, err := v1.NewBackground(ctx, cfg, &nobackup.NoBackup{}, &noecnryption.NoEncryption{})
//	require.NoError(t, err)
//	defer env.Stop()
//
//	allEvents, err := env.Launcher().Subscribe(ctx, 100, true)
//	require.NoError(t, err)
//	core.LogEvents(internal.LoggerFromContext(ctx), allEvents)
//
//	const port = "29931"
//	const bindAddr = "127.0.0.1:" + port
//
//	err = env.Launcher().Launch(ctx, core.Descriptor{
//		Name: "@router",
//		Daemon: router.New(router.Config{
//			Bind: bindAddr,
//		}),
//	})
//	require.NoError(t, err)
//
//	err = env.Launcher().Launch(ctx, core.Descriptor{
//		Name: "@poller/my-project",
//		Daemon: pipe.Poller(src, time.Second, time.Minute, false, tmpDir, map[string]string{
//			"MY_TEST": "123",
//		}),
//	})
//	require.NoError(t, err)
//
//	ready := core.WaitForEvent(ctx, env.Launcher(), "my-project", core.LauncherEventReady)
//	require.True(t, ready)
//
//	u := "http://my-project.localhost:" + port
//	t.Log(u)
//	var ok bool
//	for i := 0; i < 10; i++ {
//		res, err := http.Get(u)
//		if err != nil || res.StatusCode == http.StatusBadGateway {
//			t.Log("attempt", i, "failed:", err)
//			time.Sleep(time.Second)
//			continue
//		}
//		ok = true
//		break
//	}
//
//	assert.True(t, ok)
//	res, err := http.Get("http://my-project.localhost:" + port)
//	assert.NoError(t, err)
//	data, err := ioutil.ReadAll(res.Body)
//	assert.NoError(t, err)
//	assert.Equal(t, http.StatusOK, res.StatusCode)
//	assert.Equal(t, "web\n", string(data))
//
//	res, err = http.Get("http://web.my-project.localhost:" + port)
//	assert.NoError(t, err)
//	data, err = ioutil.ReadAll(res.Body)
//	assert.NoError(t, err)
//	assert.Equal(t, http.StatusOK, res.StatusCode)
//	assert.Equal(t, "web\n", string(data))
//
//	res, err = http.Get("http://srv.my-project.localhost:" + port)
//	assert.NoError(t, err)
//	data, err = ioutil.ReadAll(res.Body)
//	assert.NoError(t, err)
//	assert.Equal(t, http.StatusOK, res.StatusCode)
//	assert.Equal(t, "srv\n", string(data))
//
//	res, err = http.Post("http://env.my-project.localhost:"+port+"/env", "", nil)
//	assert.NoError(t, err)
//	data, err = ioutil.ReadAll(res.Body)
//	assert.NoError(t, err)
//	assert.Equal(t, http.StatusOK, res.StatusCode)
//	assert.Contains(t, string(data), "123")
//}
