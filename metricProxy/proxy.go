package metricProxy

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/wrouesnel/reverse_exporter/config"
	"github.com/prometheus/common/model"
	"errors"
	"net/url"
	"context"
)

var (
	ErrNameFieldOverrideAttempted = errors.New("cannot override name field with additional labels")
	ErrFileProxyScrapeError = errors.New("file proxy file read failed")
)

// MetricProxy presents an interface which allows a context-cancellable scrape of a backend proxy
type MetricProxy interface {
	// Scrape returns the metrics.
	Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error)
}

// ReverseProxyEndpoint wraps a collection of MetricProxy's and handles rewriting their labels to convert them into
// a consistent metric interface.
type ReverseProxyEndpoint struct {

}

// NewMetricReverseProxy initializes a new reverse proxy from the given configuration.
func NewMetricReverseProxy(exporter config.ReverseExporter) (MetricProxy, error) {


	labels := make(model.LabelSet)
	labels[reverseProxyNameLabel] = model.LabelValue(name)

	for k, v := range addnlabels {
		if k == reverseProxyNameLabel {
			return nil, ErrNameFieldOverrideAttempted
		}
		labels[model.LabelName(k)] = model.LabelValue(v)
	}

	return MetricProxy(&MetricReverseProxy{
		address:  address,
		deadline: deadline,
		labels:   labels,
	}), nil
}
