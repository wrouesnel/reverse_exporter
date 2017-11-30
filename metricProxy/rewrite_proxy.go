package metricProxy

import (
	"context"
	"net/url"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

var _ MetricProxy = &rewriteProxy{}

// rewriteProxy implements the MetricProxy interface by proxing to another proxy
// and rewriting the metrics it returns.
type rewriteProxy struct {
	proxy MetricProxy
	labels model.LabelSet
}

// Scrape scrapes using the underlying metric proxy, and rewrites the results with the
// attached labelset.
func (rpb *rewriteProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Derive a new context from the request
	childCtx, _ := context.WithCancel(ctx)
	// Do the metric scrape
	mfs, err := rpb.proxy.Scrape(childCtx, values)
	if err != nil {
		return nil, err
	}
	// Rewrite the metric set before returning it.
	rewriteMetrics(rpb.labels, mfs)
	return mfs, nil
}