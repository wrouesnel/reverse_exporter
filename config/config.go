package config

import (
	. "github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

// TODO: error if a user tries to override this with labels
const exporterNameLabel = "exporter_name"

// Load parses the given string as a YAML ExporterConfig
func Load(s string) (*ExporterConfig, error) {
	cfg := new(ExporterConfig)

	// Important: we treat the yaml file as a big list, and unmarshal to our
	// big list here.
	err := yaml.Unmarshal([]byte(s), cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadFromFile loads an ExporterConfig from the given filepath
func LoadFromFile(filename string) (*ExporterConfig, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Load(string(content))
}

// Save dumps an ExporterConfig as a YAML file.
func Save(cfg *ExporterConfig) ([]byte, error) {
	out, err := yaml.Marshal(cfg)
	return out, err
}

type AuthType string

const (
	AuthTypeNone AuthType = ""
	AuthTypeBasic AuthType = "basic"
)
// ExporterConfig is the global configuration.
type ExporterConfig struct {
	ReversedExporters []ReverseExporter `yaml:"reverse_exporters"`
}

// ReverseExporter is a configuration struct describing a logically-decoded proxied exporter
type ReverseExporter struct {
	// Path is the URL path this set of exporters will be found under.
	Path string `yaml:"path"`
	// Exporters is a list of URLs defining exporter endpoints to be aggregated
	// and the unique name to be given to differentiate their metrics.
	Exporters []Exporter `yaml:"exporters"`
	// AuthType is the type of authentication backend to use for this reverse
	// proxy. Currently only nothing and "basic" are supported.
	AuthType AuthType	`yaml:"auth_type"`
	// HtPasswdFile is the HtPasswd file to use for basic auth if basic auth is
	// requested.
	HtPasswdFile string `yaml:"htpasswd_file"`
}

type Exporter struct {
	// Name is the
	Name string `yaml:"name"`
	// A URI giving the address the exporter is found at.
	// HTTP: http://localhost/metrics
	// Unix: http://unix:/path/to/socket:/metrics
	// File: file:///var/lib/file_metrics.prom
	// Exec: exec:///usr/local/bin/exporter_script
	Address string `yaml:"address"`
	// Deadline is the maximum length of time connecting to and retrieving the
	// results of this exporter can take.
	Deadline Duration          `yaml:"proxy_timeout"`
	// Labels is a static set of additional metric labels to add during metric
	// rewriting. It can not be used to supercede the name label.
	Labels   map[string]string `yaml:"labels"`
	// ForwardUrlParams determines whether the exporter will have ALL url params
	// of the parent request added to it.
	ForwardUrlParams bool	   `yaml:"forward_url_params"`
}
