package metricproxy

import (
	"context"
	"net/url"

	"github.com/pkg/errors"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

var _ MetricProxy = &rewriteProxy{}

// rewriteProxy implements the MetricProxy interface by proxying to another proxy
// and rewriting the metrics it returns.
type rewriteProxy struct {
	proxy  MetricProxy
	labels model.LabelSet
}

// Scrape scrapes using the underlying metric proxy, and rewrites the results with the
// attached labelset.
func (rpb *rewriteProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Derive a new context from the request
	childCtx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	// Do the metric scrape
	mfs, err := rpb.proxy.Scrape(childCtx, values)
	if err != nil {
		return nil, errors.Wrap(err, "underlying metric proxy scrape error")
	}
	// Rewrite the metric set before returning it.
	rewriteMetrics(rpb.labels, mfs)
	return mfs, nil
}
