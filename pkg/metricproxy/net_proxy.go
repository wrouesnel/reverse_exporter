package metricproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pkg/errors"

	"go.uber.org/zap"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/wrouesnel/reverse_exporter/version"
)

const (
	contentTypeHeader     = "Content-Type"
	contentLengthHeader   = "Content-Length"
	contentEncodingHeader = "Content-Encoding"
	acceptEncodingHeader  = "Accept-Encoding"
)

const acceptHeader = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.7,text/plain;version=0.0.4;q=0.3,*/*;q=0.1`

const reverseProxyNameLabel = "exporter_name"

var userAgentHeader = fmt.Sprintf("Prometheus Reverse Exporter/%s", version.Version) //nolint:gochecknoglobals

var bufPool sync.Pool //nolint:gochecknoglobals

// ensure netProxy implements MetricProxy.
var _ MetricProxy = &netProxy{}

type netProxy struct {
	address            string
	deadline           time.Duration
	forwardQueryParams bool
	log                *zap.Logger
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
func scrape(ctx context.Context, deadline time.Duration, address string, values url.Values) ([]*dto.MetricFamily, error) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "creating HTTP request failed")
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Set("User-Agent", userAgentHeader)
	//req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", fmt.Sprintf("%f", s.timeout.Seconds()))

	// Replace query parameters only if specified
	if values != nil {
		req.URL.RawQuery = values.Encode()
	}

	// If a non-zero deadline is specificed, derive a new context - otherwise just
	// pass the request deadline through.
	var proxyCtx context.Context
	if deadline != 0 {
		childCtx, cancelFn := context.WithTimeout(ctx, deadline)
		defer cancelFn()
		proxyCtx = childCtx
	} else {
		proxyCtx = ctx
	}

	resp, err := http.DefaultClient.Do(req.WithContext(proxyCtx))
	if err != nil {
		return nil, errors.Wrap(err, "http scrape failure")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrapf(ErrNetProxyScrapeError, "server returned HTTP status %s", resp.Status)
	}

	mfs, err := decodeMetrics(resp.Body, expfmt.ResponseFormat(resp.Header))
	return mfs, err
}
