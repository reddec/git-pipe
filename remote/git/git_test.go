package git_test

import (
	"context"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/reddec/git-pipe/remote/git"

	"github.com/stretchr/testify/assert"
)

func TestPoller(t *testing.T) {
	u, err := url.Parse("https://github.com/reddec/tinc-boot.git")
	assert.NoError(t, err)

	dir, err := ioutil.TempDir("", "")
	if !assert.NoError(t, err) {
		return
	}

	defer os.RemoveAll(dir)

	poller := git.New(*u)

	changed, err := poller.Poll(context.Background(), dir)
	assert.NoError(t, err)
	assert.True(t, changed)

	changed, err = poller.Poll(context.Background(), dir)
	assert.NoError(t, err)
	assert.False(t, changed)

	assert.DirExists(t, filepath.Join(dir, ".git"))
	assert.FileExists(t, filepath.Join(dir, "README.md"))
}
