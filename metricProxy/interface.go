package metricProxy

import (
	"context"
	"errors"
	"github.com/prometheus/common/model"
	"github.com/wrouesnel/reverse_exporter/config"
	"net/url"
)

var (
	ErrNameFieldOverrideAttempted = errors.New("cannot override name field with additional labels")
	ErrFileProxyScrapeError       = errors.New("file proxy file read failed")
)

// MetricProxy presents an interface which allows a context-cancellable scrape of a backend proxy
type MetricProxy interface {
	// Scrape returns the metrics.
	Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error)
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
