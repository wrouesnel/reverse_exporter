package config

import (
	"errors"
	"io/ioutil"

	"time"

	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

// TODO: error if a user tries to override this with labels
//const exporterNameLabel = "exporter_name"

// nolint: golint
var (
	ErrInvalidExportersConfig = errors.New("exporters key is not in the known format")
	ErrUnknownExporterType    = errors.New("unknown exporter type specified")
)

var (
	// DefaultNetTimeout is the default timeout set for remote network exporters.
	DefaultNetTimeout = model.Duration(time.Second * 1)
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

// AuthType is one of several constants used to specify the type of authentication a reverse
// proxy endpoint should have. The default empty string means no auth.
type AuthType string

// nolint: golint
const (
	AuthTypeNone  AuthType = ""      // no authentication
	AuthTypeBasic AuthType = "basic" // basic authentication
)

// ExporterConfig is the global configuration.
type ExporterConfig struct {
	ReverseExporters []ReverseExporter `yaml:"reverse_exporters"`
	// Catch-all to error on invalid config
	XXX map[string]interface{} `yaml:",inline,omitempty"`
}

// ReverseExporter is a configuration struct describing a logically-decoded proxied exporter
type ReverseExporter struct {
	// Path is the URL path this set of exporters will be found under.
	Path string `yaml:"path"`
	// Exporters is a list of URLs defining exporter endpoints to be aggregated
	// and the unique name to be given to differentiate their metrics.
	Exporters []interface{} `yaml:"exporters"`
	// AuthType is the type of authentication backend to use for this reverse
	// proxy. Currently only nothing and "basic" are supported.
	AuthType AuthType `yaml:"auth_type"`
	// HtPasswdFile is the HtPasswd file to use for basic auth if basic auth is
	// requested.
	HtPasswdFile string `yaml:"htpasswd_file"`
}

// UnmarshalYAML implements yaml.Unmarshaller
func (re *ReverseExporter) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Unmarshal most of the config naturally
	type plain ReverseExporter
	err := unmarshal((*plain)(re))
	if err != nil {
		return err
	}

	// Post-process the exporters section
	for idx, rawExporter := range re.Exporters {
		exporterMap, ok := rawExporter.(map[interface{}]interface{})

		if len(exporterMap) > 1 || !ok || len(exporterMap) == 0 {
			return ErrInvalidExportersConfig
		}

		var exporterType string
		var exporterConfig interface{}
		for k, v := range exporterMap {
			s, ok := k.(string)
			if !ok {
				return ErrInvalidExportersConfig
			}
			exporterType = s
			exporterConfig = v
			break
		}

		// Remarshal the exporter config to YAML so it can be decoded explicitly
		// into a config object below.
		exporterConfigYAML, yerr := yaml.Marshal(exporterConfig)
		if yerr != nil {
			return yerr
		}

		var parsedConfig interface{}

		switch exporterType {
		case "file":
			config := FileExporterConfig{}
			perr := yaml.Unmarshal(exporterConfigYAML, &config)
			if perr != nil {
				return perr
			}
			parsedConfig = config
		case "exec":
			config := ExecExporterConfig{}
			perr := yaml.Unmarshal(exporterConfigYAML, &config)
			if perr != nil {
				return perr
			}
			parsedConfig = config
		case "exec-cached":
			config := ExecCachingExporterConfig{}
			perr := yaml.Unmarshal(exporterConfigYAML, &config)
			if perr != nil {
				return perr
			}
			parsedConfig = config
		case "http":
			config := HTTPExporterConfig{}
			perr := yaml.Unmarshal(exporterConfigYAML, &config)
			if perr != nil {
				return perr
			}
			parsedConfig = config
		default:
			return ErrUnknownExporterType
		}

		re.Exporters[idx] = parsedConfig
	}

	return nil
}

// BaseExporter is the interface all exporters must implement
type BaseExporter interface {
	GetBaseExporter() Exporter
}

// Exporter implements BaseExporter
type Exporter struct {
	// Name is the name of the underlying exporter which will be appended to the metrics
	Name string `yaml:"name"`
	// NoRewrite disables appending of the name (explicit labels will be appended however)
	NoRewrite bool `yaml:"no_rewrite"`
	// Labels are additional key-value labels which should be statically added to all metrics
	Labels map[string]string `yaml:"labels"`
}

// GetBaseExporter returns the common exporter parameters of an exporter
// TODO: make correctly read-only
func (e Exporter) GetBaseExporter() Exporter {
	return e
}

// FileExporterConfig contains configuration specific to reverse proxying files
type FileExporterConfig struct {
	Path     string `yaml:"path"`
	Exporter `yaml:",inline"`
}

// UnmarshalYAML implements yaml.Unmarshaller
func (fec *FileExporterConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain FileExporterConfig
	return unmarshal((*plain)(fec))
}

// ExecExporterConfig contains configuration specific to reverse proxying executable scripts
type ExecExporterConfig struct {
	Command  string   `yaml:"command"`
	Args     []string `yaml:"args"`
	Exporter `yaml:",inline"`
}

// UnmarshalYAML implements yaml.Unmarshaller
func (eec *ExecExporterConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ExecExporterConfig
	return unmarshal((*plain)(eec))
}

// ExecCachingExporterConfig contains configuration specific to reverse proxying cached executable scripts
type ExecCachingExporterConfig struct {
	Command      string         `yaml:"command"`
	Args         []string       `yaml:"args"`
	ExecInterval model.Duration `yaml:"exec_interval"`
	Exporter     `yaml:",inline"`
	//ExecExporterConfig `yaml:",inline"`
}

// UnmarshalYAML implements yaml.Unmarshaller
func (ecec *ExecCachingExporterConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ExecCachingExporterConfig
	return unmarshal((*plain)(ecec))
}

// HTTPExporterConfig contains configuration specific to reverse proxying normal http-based Prometheus exporters
type HTTPExporterConfig struct {
	// A URI giving the address the exporter is found at.
	// HTTP: http://localhost/metrics
	// Unix: http://unix:/path/to/socket:/metrics
	Address string `yaml:"address"`
	// Timeout is the maximum length of time connecting to and retrieving the
	// results of this exporter can take.
	Timeout model.Duration `yaml:"timeout,omitempty"`
	// ForwardURLParams determines whether the exporter will have ALL url params
	// of the parent request added to it.
	ForwardURLParams bool `yaml:"forward_url_params"`
	Exporter         `yaml:",inline"`
}

// UnmarshalYAML implements yaml.Unmarshaller
func (hec *HTTPExporterConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HTTPExporterConfig

	hec.Timeout = DefaultNetTimeout

	return unmarshal((*plain)(hec))
}
