package v1

import "time"

type Config struct {
	NetworkName         string        // local network name
	RetryDeployInterval time.Duration // interval between attempts to start
	GracefulTimeout     time.Duration // timeout for cleanup
	TempDir             string        // storage temp dir
	Driver              string        // volumes driver. Most common - local
}

func DefaultConfig() Config {
	return Config{
		NetworkName:         "git-pipe",
		RetryDeployInterval: 5 * time.Second,
		GracefulTimeout:     30 * time.Second,
		Driver:              "local",
	}
}
