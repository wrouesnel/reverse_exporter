package metricProxy

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

const acceptHeader = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3,*/*;q=0.1`

const reverseProxyNameLabel = "exporter_name"

var userAgentHeader = fmt.Sprintf("Prometheus Reverse Exporter/%s", version.Version)

var bufPool sync.Pool

// ensure netProxy implements MetricProxy
var _ MetricProxy = &netProxy{}

type netProxy struct {
	address  string
	deadline time.Duration
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (mrp *netProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Derive a new context from the request
	childCtx, _ := context.WithCancel(ctx)
	mfs, err := scrape(childCtx, mrp.deadline, mrp.address)
	if err != nil {
		return nil, err
	}
	return mfs, nil
}

// scrape decodes MetricFamily's from the wire format, and returns them ready to be proxied.
func scrape(ctx context.Context, deadline time.Duration, address string) ([]*dto.MetricFamily, error) {
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Set("User-Agent", userAgentHeader)
	// TODO: pass through this value ?
	//req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", fmt.Sprintf("%f", s.timeout.Seconds()))

	// FIXME: specify a real client
	resp, err := ctxhttp.Do(ctx, http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP status %s", resp.Status)
	}

	mfs, err := decodeMetrics(resp.Body, expfmt.ResponseFormat(resp.Header))
	return mfs, err
}
