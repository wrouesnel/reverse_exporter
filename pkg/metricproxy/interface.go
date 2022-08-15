package metricproxy

import (
	"context"
	"fmt"
	"net/url"

	"github.com/pkg/errors"
	"github.com/wrouesnel/reverse_exporter/pkg/config"
	"github.com/wrouesnel/reverse_exporter/pkg/middleware/auth"
	"go.uber.org/zap"

	"net/http"
	"time"

	"github.com/prometheus/common/model"

	dto "github.com/prometheus/client_model/go"
)

var (
	ErrNameFieldOverrideAttempted = errors.New("cannot override name field with additional labels")
	ErrFileProxyScrapeError       = errors.New("file proxy file read failed")
	ErrNetProxyScrapeError        = errors.New("HTTP proxy failed to read backend")
	ErrUnknownExporterType        = errors.New("cannot configure unknown exporter type")
	ErrExporterNameUsedTwice      = errors.New("cannot use the same exporter name twice for one endpoint")
)

// MetricProxy presents an interface which allows a context-cancellable scrape of a backend proxy.
type MetricProxy interface {
	// Scrape returns the metrics.
	Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error)
}

// NewMetricReverseProxy initializes a new reverse proxy from the given configuration.
//nolint:cyclop
func NewMetricReverseProxy(reverseExporter *config.ReverseExporterConfig) (http.Handler, error) {
	log := zap.L().With(zap.String("path", reverseExporter.Path))

	// Initialize a basic reverse proxy
	backend := &ReverseProxyEndpoint{
		metricPath: reverseExporter.Path,
		backends:   make([]MetricProxy, 0),
	}
	backend.handler = backend.serveMetricsHTTP

	usedNames := make(map[string]struct{})

	// Start adding backends
	for _, exporter := range reverseExporter.Exporters.All() {
		var newExporter MetricProxy

		baseExporter := exporter.GetBaseExporter()
		eLog := log.With(zap.String("name", baseExporter.Name))

		//nolint:varnamelen
		switch e := exporter.(type) {
		case *config.FileExporterConfig:
			eLog.Debug("Adding new file reverseExporter proxy")
			newExporter = newFileProxy(e)
		case *config.ExecExporterConfig:
			eLog.Debug("Adding new exec reverseExporter proxy")
			newExporter = newExecProxy(e)
		case *config.ExecCachingExporterConfig:
			eLog.Debug("Adding new caching exec reverseExporter proxy")
			newExporter = newExecCachingProxy(e)
		case *config.HTTPExporterConfig:
			eLog.Debug("Adding new http reverseExporter proxy")
			newExporter = &netProxy{
				address:            e.Address,
				deadline:           time.Duration(e.Timeout),
				forwardQueryParams: e.ForwardURLParams,
			}
		default:
			eLog.Error("Unknown proxy configuration item found", zap.String("type", fmt.Sprintf("%T", e)))
			return nil, ErrUnknownExporterType
		}

		// Got reverseExporter, now add a rewrite proxy in front of it
		labels := make(model.LabelSet)

		// Keep track of reverseExporter name use to pre-empt collisions
		if _, found := usedNames[baseExporter.Name]; !found {
			usedNames[baseExporter.Name] = struct{}{}
		} else {
			eLog.Error("Exporter name re-use even if rewrite is disabled is not allowed")
			return nil, ErrExporterNameUsedTwice
		}

		// If not rewriting, eLog it.
		if !baseExporter.NoRewrite {
			labels[reverseProxyNameLabel] = model.LabelValue(baseExporter.Name)
		} else {
			eLog.Debug("Disabled explicit reverseExporter name")
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

	var err error
	backend.handler, err = auth.SetupAuthHandler(reverseExporter.Auth, backend.handler)
	if err != nil {
		return backend, errors.Wrapf(err, "failed configuring reverseExporter auth: %s",
			reverseExporter.Path)
	}

	return backend, nil
}
