package config

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	. "github.com/prometheus/common/model"
)

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

// ExporterConfig is the global configuration.
type ExporterConfig struct {
	ReverseExporters []ReverseExporter `yaml:"reverse_exporters""`
}

// ReverseExporter is a configuration struct describing a logically-decoded proxied exporter
type ReverseExporter struct {
	// Name is applied as a label to metrics proxied from this endpoint. It must be unique, and is applied as
	// exported_name by default. This allows differentiating subsystems in an appliance.
	Name string `yaml:"name"`
	// A URI giving the address the exporter is found at.
	// HTTP: http://localhost/metrics
	Address string `yaml:"address"`
	// Deadline is the maximum length of time connecting to and retrieving the results of this exporter can take.
	Deadline Duration `yaml:"proxy_timeout"`
}
