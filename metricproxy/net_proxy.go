package metricproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/wrouesnel/reverse_exporter/version"
	"golang.org/x/net/context/ctxhttp"
)

const (
	contentTypeHeader     = "Content-Type"
	contentLengthHeader   = "Content-Length"
	contentEncodingHeader = "Content-Encoding"
	acceptEncodingHeader  = "Accept-Encoding"
)

const acceptHeader = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3,*/*;q=0.1` // nolint: lll

const reverseProxyNameLabel = "exporter_name"
const authRealm = "Secured"

var userAgentHeader = fmt.Sprintf("Prometheus Reverse Exporter/%s", version.Version)

var bufPool sync.Pool

// ensure netProxy implements MetricProxy
var _ MetricProxy = &netProxy{}

type netProxy struct {
	address            string
	deadline           time.Duration
	forwardQueryParams bool
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (mrp *netProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Derive a new context from the request
	childCtx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	requestValues := url.Values{}
	if mrp.forwardQueryParams {
		requestValues = values
	}

	mfs, err := scrape(childCtx, mrp.deadline, mrp.address, requestValues)
	if err != nil {
		return nil, err
	}
	return mfs, nil
}

// scrape decodes MetricFamily's from the wire format, and returns them ready to be proxied.
func scrape(ctx context.Context, deadline time.Duration, address string, values url.Values) ([]*dto.MetricFamily, error) { // nolint: lll
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Set("User-Agent", userAgentHeader)
	// TODO: pass through this value ?
	//req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", fmt.Sprintf("%f", s.timeout.Seconds()))

	// Replace query parameters only if specified
	if values != nil {
		req.URL.RawQuery = values.Encode()
	}

	// Derive a new context with a deadline
	childCtx, cancelFn := context.WithTimeout(ctx, deadline)
	defer cancelFn()

	resp, err := ctxhttp.Do(childCtx, http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // nolint: errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP status %s", resp.Status)
	}

	mfs, err := decodeMetrics(resp.Body, expfmt.ResponseFormat(resp.Header))
	return mfs, err
}
