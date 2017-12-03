package metricProxy

import (
	"context"
	"errors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/wrouesnel/reverse_exporter/config"
	"net/url"
	"github.com/golang/protobuf/proto"
	"sort"
	"strings"
	"io"
	"bytes"
	"compress/gzip"
	"net/http"
	"github.com/wrouesnel/go.log"
	"sync"
	"github.com/prometheus/common/expfmt"
	"fmt"
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
		go func(mfsCh chan<- []*dto.MetricFamily) {
			defer wg.Done()
			mfs, err := backend.Scrape(ctx, req.URL.Query())
			if err != nil {
				log.WithError(err).Errorln("Error while scraping backend handler for endpoint")
				// TODO: emit a "scrape failed" metric of some sort
				return
			}
			mfsCh <- mfs
		}(mfsCh)
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
	HandleSerializeMetrics(wr, req, allMfs)
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



// HandleSerializeMetrics writes the samples as metrics to the given http.ResponseWriter
func HandleSerializeMetrics(w http.ResponseWriter, req *http.Request, mfs []*dto.MetricFamily) {
	contentType := expfmt.Negotiate(req.Header)
	buf := getBuf()
	defer giveBuf(buf)
	writer, encoding := decorateWriter(req, buf)
	enc := expfmt.NewEncoder(writer, contentType)
	var lastErr error
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			lastErr = err
			http.Error(w, "An error has occurred during metrics encoding:\n\n"+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if closer, ok := writer.(io.Closer); ok {
		closer.Close()
	}
	if lastErr != nil && buf.Len() == 0 {
		http.Error(w, "No metrics encoded, last error:\n\n"+lastErr.Error(), http.StatusInternalServerError)
		return
	}
	header := w.Header()
	header.Set(contentTypeHeader, string(contentType))
	header.Set(contentLengthHeader, fmt.Sprint(buf.Len()))
	if encoding != "" {
		header.Set(contentEncodingHeader, encoding)
	}
	w.Write(buf.Bytes())
}

// decorateWriter wraps a writer to handle gzip compression if requested.  It
// returns the decorated writer and the appropriate "Content-Encoding" header
// (which is empty if no compression is enabled).
func decorateWriter(request *http.Request, writer io.Writer) (io.Writer, string) {
	header := request.Header.Get(acceptEncodingHeader)
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part := strings.TrimSpace(part)
		if part == "gzip" || strings.HasPrefix(part, "gzip;") {
			return gzip.NewWriter(writer), "gzip"
		}
	}
	return writer, ""
}

func getBuf() *bytes.Buffer {
	buf := bufPool.Get()
	if buf == nil {
		return &bytes.Buffer{}
	}
	return buf.(*bytes.Buffer)
}

func giveBuf(buf *bytes.Buffer) {
	buf.Reset()
	bufPool.Put(buf)
}
