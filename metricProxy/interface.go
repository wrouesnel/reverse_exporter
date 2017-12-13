package metricProxy

import (
	"context"
	"errors"
	"net/url"

	"github.com/prometheus/common/model"
	"github.com/wrouesnel/reverse_exporter/config"

	"net/http"
	"time"

	"github.com/abbot/go-http-auth"
	dto "github.com/prometheus/client_model/go"
	log "github.com/prometheus/common/log"
)

// nolint: golint
var (
	ErrNameFieldOverrideAttempted = errors.New("cannot override name field with additional labels")
	ErrFileProxyScrapeError       = errors.New("file proxy file read failed")
	ErrUnknownExporterType        = errors.New("cannot configure unknown exporter type")
	ErrExporterNameUsedTwice      = errors.New("cannot use the same exporter name twice for one endpoint")
)

// MetricProxy presents an interface which allows a context-cancellable scrape of a backend proxy
type MetricProxy interface {
	// Scrape returns the metrics.
	Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error)
}

// NewMetricReverseProxy initializes a new reverse proxy from the given configuration.
func NewMetricReverseProxy(exporter config.ReverseExporter) (http.Handler, error) {
	log := log.With("path", exporter.Path) // nolint: vetshadow

	// Initialize a basic reverse proxy
	backend := &ReverseProxyEndpoint{
		metricPath: exporter.Path,
		backends:   make([]MetricProxy, 0),
	}
	backend.handler = backend.serveMetricsHTTP

	usedNames := make(map[string]struct{})

	// Start adding backends
	for _, exporter := range exporter.Exporters {
		var newExporter MetricProxy

		baseExporter := exporter.(config.BaseExporter).GetBaseExporter()
		log := log.With("name", baseExporter.Name) // nolint: vetshadow

		switch e := exporter.(type) {
		case config.FileExporterConfig:
			log.Debugln("Adding new file exporter proxy")
			newExporter = newFileProxy(&e)
		case config.ExecExporterConfig:
			log.Debugln("Adding new exec exporter proxy")
			newExporter = newExecProxy(&e)
		case config.ExecCachingExporterConfig:
			log.Debugln("Adding new caching exec exporter proxy")
			newExporter = newExecCachingProxy(&e)
		case config.HTTPExporterConfig:
			log.Debugln("Adding new http exporter proxy")
			newExporter = &netProxy{
				address:            e.Address,
				deadline:           time.Duration(e.Timeout),
				forwardQueryParams: e.ForwardURLParams,
			}
		default:
			log.Errorf("Unknown proxy configuration item found: %T", e)
			return nil, ErrUnknownExporterType
		}

		// Got exporter, now add a rewrite proxy in front of it
		labels := make(model.LabelSet)

		// Keep track of exporter name use to pre-empt collisions
		if _, found := usedNames[baseExporter.Name]; !found {
			usedNames[baseExporter.Name] = struct{}{}
		} else {
			log.Errorln("Exporter name re-use even if rewrite is disabled is not allowed")
			return nil, ErrExporterNameUsedTwice
		}

		// If not rewriting, log it.
		if !baseExporter.NoRewrite {
			labels[reverseProxyNameLabel] = model.LabelValue(baseExporter.Name)
		} else {
			log.Debugln("Disabled explicit exporter name for", baseExporter.Name)
		}

		// Set the additional labels.
		for k, v := range baseExporter.Labels {
			if k == reverseProxyNameLabel {
				return nil, ErrNameFieldOverrideAttempted
			}
			labels[model.LabelName(k)] = model.LabelValue(v)
		}

		// Configure the rewriting proxy shim.
		rewriteProxy := &rewriteProxy{
			proxy:  newExporter,
			labels: labels,
		}

		// Add the new backend to the endpoint
		backend.backends = append(backend.backends, rewriteProxy)
	}

	// Process the auth configuration
	switch exporter.AuthType {
	case config.AuthTypeNone:
		log.Debugln("No authentication for endpoint")
	case config.AuthTypeBasic:
		log.Debugln("Adding basic auth to endpoint")

		provider := auth.HtpasswdFileProvider(exporter.HtPasswdFile)
		authenticator := auth.NewBasicAuthenticator(authRealm, provider)

		authHandler := func(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
			backend.handler(w, &r.Request)
		}
		backend.handler = authenticator.Wrap(authHandler)

	default:
		log.Errorln("Unknown auth-type specified:", exporter.AuthType)
	}

	return backend, nil
}
