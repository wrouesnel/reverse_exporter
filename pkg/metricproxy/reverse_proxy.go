package metricproxy

import (
	"net/http"
	"sync"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/log"
)

// ReverseProxyEndpoint wraps a collection of ReverseProxyBackends. It exposes an HTTP endpoint
// able to be passed to the user and handles routing and authentication.
type ReverseProxyEndpoint struct {
	// metricPath is the path this RPE endpoint is being proxied under
	metricPath string
	// backends is a list of metric proxy's currently under this backend
	backends []MetricProxy
	// handler is the (possibly wrapped) function which provides the real ServeHTTP
	handler http.HandlerFunc
}

// ServeHTTP implements http.Handler by calling the designated wrapper function.
func (rpe *ReverseProxyEndpoint) ServeHTTP(wr http.ResponseWriter, req *http.Request) {
	rpe.handler(wr, req)
}

// serveMetricsHTTP implements http.Handler. Specifically: it serves the aggregated rewritten
// Prometheus endpoints contained underneath it. This function is the direct handler -
// ServeHTTP on the interface varies based on the other wrappers used to construct it.
func (rpe *ReverseProxyEndpoint) serveMetricsHTTP(wr http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// As an appliance, we return nothing till we know the result of our reverse
	// proxied metrics.
	wg := new(sync.WaitGroup)
	// This channel is guarded by wg - it will be closed when the waitgroup
	// finishes.
	mfsCh := make(chan []*dto.MetricFamily)
	mfsResultCh := make(chan []*dto.MetricFamily)

	// On request, request all included exporters to return values.
	log.Debugln("Scraping", len(rpe.backends), "exporters")
	for _, backend := range rpe.backends {
		wg.Add(1)
		go func(mfsCh chan<- []*dto.MetricFamily, backend MetricProxy) {
			defer wg.Done()
			mfs, err := backend.Scrape(ctx, req.URL.Query())
			if err != nil {
				log.With("error", err).Errorln("Error while scraping backend handler for endpoint")
				// TODO: emit a "scrape failed" metric of some sort
				return
			}
			mfsCh <- mfs
		}(mfsCh, backend)
	}
	// metric aggregator combines all the scraped metrics and emits them to the
	// result channel.
	// TODO: find and emit a metric for metric name clashes here.
	go func(chan<- []*dto.MetricFamily) {
		mfs := []*dto.MetricFamily{}
		for inpMfs := range mfsCh {
			mfs = append(mfs, inpMfs...)
		}
		mfsResultCh <- mfs
		close(mfsResultCh)
	}(mfsResultCh)

	// Wait for all scrapers to return and results to aggregate
	log.Debugln("Waiting for scrapers to return")
	wg.Wait()
	close(mfsCh)
	// collect results from mfsResultCh
	allMfs := <-mfsResultCh
	// serialize the resulting metrics to the Prometheus format and return them
	handleSerializeMetrics(wr, req, allMfs)
}
