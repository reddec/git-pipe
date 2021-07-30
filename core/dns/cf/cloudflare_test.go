package cf_test

import (
	"context"
	"os"
	"testing"

	"github.com/reddec/git-pipe/core/dns/cf"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	token := os.Getenv("CF_TOKEN")
	if token == "" {
		t.Log("no CF token")
		t.SkipNow()
		return
	}

	domain := os.Getenv("CF_DOMAIN")
	if domain == "" {
		t.Log("no CF domain")
		t.SkipNow()
		return
	}
	ctx := context.Background()

	client, err := cf.New(ctx, cf.Config{APIToken: token})
	assert.NoError(t, err)

	t.Run("register", func(t *testing.T) {
		err := client.Register(ctx, []string{"test1." + domain, "test2." + domain})
		assert.NoError(t, err)
	})

}
