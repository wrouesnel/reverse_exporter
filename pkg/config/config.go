package config

import (
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/samber/lo"
)

// TODO: error if a user tries to override this with labels
//const exporterNameLabel = "exporter_name"

// nolint: golint
var (
	ErrInvalidExportersConfig = errors.New("exporters key is not in the known format")
	ErrUnknownExporterType    = errors.New("unknown exporter type specified")
)

// Config is the main application configuration structure
type Config struct {
	Web              *WebConfig               `mapstructure:"web,omitempty"`
	ExporterDefaults *ExporterDefaults        `mapstructure:"exporter_defaults,omitempty"`
	ReverseExporters []*ReverseExporterConfig `mapstructure:"reverse_exporters,omitempty"`
}

// BasicAuthConfig defines basic authentication credentials to accept on the web interface.
// If Password is not set, then the credential set is ignored. The password is plaintext.
type BasicAuthConfig struct {
	Username string `mapstructure:"username,omitempty"` // Username to accept
	Password string `mapstructure:"password,omitempty"` // Plain text password to accept
}

//type JWTTokenAuthConfig struct {
//	Secret    string `mapstructure:"secret"`    // JWT secret suitable for algorithm
//	Algorithm string `mapstructure:"algorithm"` // Algorithm to use
//	ID        string `mapstructure:"id"`        // ID for the token provider
//}

// AuthConfig holds the configuration of any authentication put on the exporter interface.
type AuthConfig struct {
	BasicAuthCredentials []BasicAuthConfig `mapstructure:"basic_auth,omitempty"`
	//JWTToken             []JWTTokenAuthConfig `mapstructure:"jwt_auth,omitempty"`
}

// WebConfig holds global configuration for the exporters webserver.
type WebConfig struct {
	ContextPath       string         `mapstructure:"context_path,omitempty"`
	ReadHeaderTimeout model.Duration `mapstructure:"read_header_timeout,omitempty"`
	Listen            []URL          `mapstructure:"listen,omitempty"`
}

// ReverseExporterConfig is a configuration struct describing a logically-decoded proxied exporter
type ReverseExporterConfig struct {
	// Path is the URL path this set of exporters will be found under.
	Path string `mapstructure:"path"`
	// Auth is the auth to request for this path
	Auth *AuthConfig `mapstructure:"auth,omitempty"`
	// Exporters is a list of URLs defining exporter endpoints to be aggregated
	// and the unique name to be given to differentiate their metrics.
	Exporters *ExportersConfig `mapstructure:"exporters"`
}

type ExporterDefaults struct {
	HTTPDefaults       *HTTPExporterConfig        `mapstructure:"http"`
	FileDefaults       *FileExporterConfig        `mapstructure:"file"`
	ExecDefaults       *ExecExporterConfig        `mapstructure:"exec"`
	ExecCachedDefaults *ExecCachingExporterConfig `mapstructure:"exec_cached"`
}

// ExportersConfig is the internal mapping the exporter config representation
type ExportersConfig struct {
	HTTPExporters       []*HTTPExporterConfig        `mapstructure:"http"`
	FileExporters       []*FileExporterConfig        `mapstructure:"file"`
	ExecExporters       []*ExecExporterConfig        `mapstructure:"exec"`
	ExecCachedExporters []*ExecCachingExporterConfig `mapstructure:"exec_cached"`
}

func (ex *ExportersConfig) All() []BaseExporter {
	exporters := make([]BaseExporter, 0)
	exporters = append(exporters, lo.Map(ex.HTTPExporters, func(v *HTTPExporterConfig, _ int) BaseExporter { return BaseExporter(v) })...)
	exporters = append(exporters, lo.Map(ex.FileExporters, func(v *FileExporterConfig, _ int) BaseExporter { return BaseExporter(v) })...)
	exporters = append(exporters, lo.Map(ex.ExecExporters, func(v *ExecExporterConfig, _ int) BaseExporter { return BaseExporter(v) })...)
	exporters = append(exporters, lo.Map(ex.ExecCachedExporters, func(v *ExecCachingExporterConfig, _ int) BaseExporter { return BaseExporter(v) })...)
	return exporters
}

// BaseExporter is the interface all exporters must implement
type BaseExporter interface {
	GetBaseExporter() Exporter
}

// Exporter implements BaseExporter
type Exporter struct {
	// Name is the name of the underlying exporter which will be appended to the metrics
	Name string `mapstructure:"name"`
	// NoRewrite disables appending of the name (explicit labels will be appended however)
	NoRewrite bool `mapstructure:"no_rewrite"`
	// Labels are additional key-value labels which should be statically added to all metrics
	Labels map[string]string `mapstructure:"labels"`
}

// GetBaseExporter returns the common exporter parameters of an exporter
// TODO: make correctly read-only
func (e Exporter) GetBaseExporter() Exporter {
	return e
}

// FileExporterConfig contains configuration specific to reverse proxying files
type FileExporterConfig struct {
	Exporter `mapstructure:",squash"`
	Path     string `mapstructure:"path"`
}

// ExecExporterConfig contains configuration specific to reverse proxying executable scripts
type ExecExporterConfig struct {
	Exporter `mapstructure:",squash"`
	Command  string   `mapstructure:"command"`
	Args     []string `mapstructure:"args"`
}

// ExecCachingExporterConfig contains configuration specific to reverse proxying cached executable scripts
type ExecCachingExporterConfig struct {
	Exporter     `mapstructure:",squash"`
	Command      string         `mapstructure:"command"`
	Args         []string       `mapstructure:"args"`
	ExecInterval model.Duration `mapstructure:"exec_interval"`

	//ExecExporterConfig `mapstructure:",inline"`
}

// HTTPExporterConfig contains configuration specific to reverse proxying normal http-based Prometheus exporters
type HTTPExporterConfig struct {
	Exporter `mapstructure:",squash"`
	// A URI giving the address the exporter is found at.
	// HTTP: http://localhost/metrics
	// Unix: http://unix:/path/to/socket:/metrics
	Address string `mapstructure:"address"`
	// Timeout is the maximum length of time connecting to and retrieving the
	// results of this exporter can take.
	Timeout model.Duration `mapstructure:"timeout,omitempty"`
	// ForwardURLParams determines whether the exporter will have ALL url params
	// of the parent request added to it.
	ForwardURLParams bool `mapstructure:"forward_url_params"`
}
