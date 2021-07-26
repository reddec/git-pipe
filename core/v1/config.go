package v1

import "time"

type Config struct {
	NetworkName         string        // local network name
	RetryDeployInterval time.Duration // interval between attempts to start
	GracefulTimeout     time.Duration // timeout for cleanup
	TempDir             string        // storage temp dir
	Driver              string        // volumes driver. Most common - local
	Domain              string        // root domain name (optional)
	DisableResolve      bool          // do not try to resolve addresses
}

func DefaultConfig() Config {
	return Config{
		NetworkName:         "git-pipe",
		RetryDeployInterval: 5 * time.Second,
		GracefulTimeout:     30 * time.Second,
		Driver:              "local",
	}
}

func NewConfig(options ...Option) Config {
	cfg := DefaultConfig()
	for _, opt := range options {
		opt(&cfg)
	}
	return cfg
}

type Option func(cfg *Config)

func Network(name string) Option {
	return func(cfg *Config) {
		cfg.NetworkName = name
	}
}

func NoResolve() Option {
	return func(cfg *Config) {
		cfg.DisableResolve = true
	}
}

func Domain(name string) Option {
	return func(cfg *Config) {
		cfg.Domain = name
	}
}
