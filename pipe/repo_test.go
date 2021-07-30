package pipe_test

import (
	"context"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/core/event"
	"github.com/reddec/git-pipe/core/ingress"
	"github.com/reddec/git-pipe/core/network"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote"
	"github.com/reddec/git-pipe/remote/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRepo(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	tc := testBaseEnv(t, "repo-test")
	defer tc.Close()

	source := tc.CreateRepo(ctx, map[string]string{
		"Dockerfile": `
FROM hashicorp/http-echo
EXPOSE 80
VOLUME /data
HEALTHCHECK --interval=200ms CMD ["/http-echo", "-version"]
CMD ["-listen", ":80", "-text", "hello"]
`,
	})

	timedCtx, timedCancel := context.WithTimeout(ctx, time.Minute)
	defer timedCancel()

	task := internal.Spawn(timedCtx, func(ctx context.Context) error {
		pipe.Run(ctx, source, tc.env, time.Hour)
		return nil
	})
	defer task.Stop()

	select {
	case <-timedCtx.Done():
		t.Fail()
	case <-tc.events.OnReady():
	}

	assert.Contains(t, tc.testDNS.registered, "80.repo-test")
	assert.Contains(t, tc.testDNS.registered, "repo-test")

	assert.Contains(t, tc.Ingress("80.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("repo-test").Group, "repo-test")

	assert.Equal(t, tc.backup.RestoreName, "repo-test")
	assert.Equal(t, tc.backup.RestoreVolumes, []string{"repo-test"})

	assert.Equal(t, tc.backup.ScheduleName, "repo-test")
	assert.Equal(t, tc.backup.ScheduleVolumes, []string{"repo-test"})
}

func TestRepoCompose(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	tc := testBaseEnv(t, "repo-test")
	defer tc.Close()

	source := tc.CreateRepo(ctx, map[string]string{
		"docker-compose.yaml": `version: "3"
services:
 web:
   image: hashicorp/http-echo
   command: -listen :80 -text "web"
   ports:
   - 443:443
   - 8080:80

 srv:
   image: hashicorp/http-echo
   command: -listen :80 -text "srv"
   domainname: service
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
volumes:
  alfa:
  sigma:
`,
	})

	timedCtx, timedCancel := context.WithTimeout(ctx, time.Minute)
	defer timedCancel()

	tc.env.Vars = map[string]string{
		"MY_TEST": "xyz",
	}

	task := internal.Spawn(timedCtx, func(ctx context.Context) error {
		pipe.Run(ctx, source, tc.env, time.Hour)
		return nil
	})
	defer task.Stop()

	select {
	case <-timedCtx.Done():
		t.Fail()
	case <-tc.events.OnReady():
	}

	assert.Contains(t, tc.testDNS.registered, "web.repo-test")
	assert.Contains(t, tc.testDNS.registered, "80.web.repo-test")
	assert.Contains(t, tc.testDNS.registered, "443.web.repo-test")

	assert.Contains(t, tc.testDNS.registered, "service.repo-test")
	assert.Contains(t, tc.testDNS.registered, "80.service.repo-test")

	assert.Contains(t, tc.testDNS.registered, "env.repo-test")
	assert.Contains(t, tc.testDNS.registered, "8080.env.repo-test")

	assert.Contains(t, tc.Ingress("web.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("80.web.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("443.web.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("web.repo-test").Addresses[0], ":80")

	assert.Contains(t, tc.Ingress("service.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("80.service.repo-test").Group, "repo-test")

	assert.Contains(t, tc.Ingress("env.repo-test").Group, "repo-test")
	assert.Contains(t, tc.Ingress("8080.env.repo-test").Group, "repo-test")

	assert.Equal(t, "repo-test", tc.backup.RestoreName)
	assert.Equal(t, []string{"repo-test_alfa", "repo-test_sigma"}, tc.backup.RestoreVolumes)

	assert.Equal(t, "repo-test", tc.backup.ScheduleName)
	assert.Equal(t, []string{"repo-test_alfa", "repo-test_sigma"}, tc.backup.ScheduleVolumes)
}

type testContext struct {
	backup         *mockBackup
	workDir        string
	backupDir      string
	events         *event.Emitter
	ingressBackend *testIngress
	testDNS        *testDNS
	env            *core.Environment
}

func (tc *testContext) Ingress(domain string) ingress.Record {
	for _, r := range tc.ingressBackend.state {
		if r.Domain == domain {
			return r
		}
	}
	return ingress.Record{}
}

func (tc *testContext) Close() {
	_ = tc.env.Docker.Close()
	_ = os.RemoveAll(tc.workDir)
}

func (tc *testContext) CreateRepo(ctx context.Context, files map[string]string) remote.Source {
	name := uuid.New().String()

	gitDir := filepath.Join(tc.workDir, "repos", "bare", name)
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		panic(err)
	}

	repoDir := filepath.Join(tc.workDir, "repos", "cloned", name)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		panic(err)
	}

	inBareRepo := internal.At(repoDir)

	// create git repo
	err := inBareRepo.Do(ctx, "git", "init", "--bare", name+".git").Exec()
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

	r, err := git.FromURL(gitURL)
	if err != nil {
		panic(err)
	}
	return r
}

func testBaseEnv(t *testing.T, name string) *testContext {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)

	workDir, err := ioutil.TempDir("", "")
	require.NoError(t, err)

	net, err := network.NewDockerNetwork(context.Background(), cli, "git-test-pipe", true)
	require.NoError(t, err)

	tc := &testContext{
		workDir:        workDir,
		backupDir:      filepath.Join(workDir, "backups"),
		ingressBackend: &testIngress{},
		testDNS:        &testDNS{},
		events:         event.New(10),
		backup:         &mockBackup{},
	}

	tc.env = &core.Environment{
		Name:      name,
		Directory: filepath.Join(workDir, "data"),
		Base: core.Base{
			DNS:     tc.testDNS,
			Ingress: ingress.New(tc.ingressBackend),
			Backup:  tc.backup,
			Network: net,
			Docker:  cli,
		},
		Event: tc.events,
	}
	return tc
}

type testIngress struct {
	state []ingress.Record
}

func (ti *testIngress) Set(ctx context.Context, records []ingress.Record) error {
	ti.state = records
	return nil
}

type testDNS struct {
	lock       sync.RWMutex
	registered map[string]bool
}

func (td *testDNS) Register(ctx context.Context, domains []string) error {
	td.lock.Lock()
	defer td.lock.Unlock()
	if td.registered == nil {
		td.registered = map[string]bool{}
	}
	for _, domain := range domains {
		td.registered[domain] = true
	}

	return nil
}

func (td *testDNS) State() map[string]bool {
	td.lock.RLock()
	defer td.lock.RUnlock()
	cp := make(map[string]bool)
	for d := range td.registered {
		cp[d] = true
	}
	return cp
}

type mockBackup struct {
	RestoreName     string
	RestoreVolumes  []string
	BackupName      string
	BackupVolumes   []string
	ScheduleName    string
	ScheduleVolumes []string
}

func (mb *mockBackup) Restore(ctx context.Context, name string, volumeNames []string) error {
	mb.RestoreName = name
	mb.RestoreVolumes = volumeNames
	return nil
}

func (mb *mockBackup) Backup(ctx context.Context, name string, volumeNames []string) error {
	mb.BackupName = name
	mb.BackupVolumes = volumeNames
	return nil
}

func (mb *mockBackup) Schedule(ctx context.Context, name string, volumeNames []string) *internal.Task {
	mb.ScheduleName = name
	mb.ScheduleVolumes = volumeNames
	return internal.Spawn(ctx, func(ctx context.Context) error {
		return nil
	})
}
