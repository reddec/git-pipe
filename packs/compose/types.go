package compose

type service struct {
	Ports    []string               `yaml:"ports,omitempty"`
	Scale    int                    `yaml:"scale,omitempty"`
	Domain   string                 `yaml:"x-domain,omitempty"`
	Root     bool                   `yaml:"x-root,omitempty"`
	Networks []string               `yaml:"networks,omitempty"`
	Unparsed map[string]interface{} `yaml:",inline"`
}
type network struct {
	External bool                   `yaml:"external,omitempty"`
	Unparsed map[string]interface{} `yaml:",inline"`
}
type volume struct {
	External bool                   `yaml:"external,omitempty"`
	Driver   string                 `yaml:"driver,omitempty"`
	Unparsed map[string]interface{} `yaml:",inline"`
}

type composeConfig struct {
	Services map[string]*service    `yaml:"services"`
	Domain   string                 `yaml:"x-domain,omitempty"`
	Networks map[string]network     `yaml:"networks,omitempty"`
	Volumes  map[string]volume      `yaml:"volumes,omitempty"`
	Unparsed map[string]interface{} `yaml:",inline"`
}
