package config

import _ "embed"

//go:embed default_config.yml
var defaultConfig []byte

// DefaultConfigFile returns the default embedded YAML config which sets the
// poller_defaults.
func DefaultConfigFile() []byte {
	return append(defaultConfig[:0:0], defaultConfig...)
}
