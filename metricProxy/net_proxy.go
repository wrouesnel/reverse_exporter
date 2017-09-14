package metricProxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/wrouesnel/go.log"
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



type MetricReverseProxy struct {
	address  string
	deadline time.Duration
	labels   model.LabelSet
}

// NewMetricReverseProxy initializes a networked metric proxy
func NewMetricReverseProxy(deadline time.Duration, address string, name string, addnlabels map[string]string) (MetricProxy, error) {
	labels := make(model.LabelSet)
	labels[reverseProxyNameLabel] = model.LabelValue(name)

	for k, v := range addnlabels {
		if k == reverseProxyNameLabel {
			return nil, errors.New("cannot override name field with additional labels")
		}
		labels[model.LabelName(k)] = model.LabelValue(v)
	}

	return MetricProxy(&MetricReverseProxy{
		address:  address,
		deadline: deadline,
		labels:   labels,
	}), nil
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (mrp *MetricReverseProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Derive a new context from the request
	childCtx, _ := context.WithCancel(ctx)
	mfs, err := scrape(childCtx, mrp.deadline, mrp.address)
	if err != nil {
		return nil, err
	}
	// rewrite with the name of this exporter
	rewriteMetrics(mrp.labels, mfs)
	return mfs, nil
}

func (mrp *MetricReverseProxy) Name() string {
	return string(mrp.labels[reverseProxyNameLabel])
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

// rewriteMetrics adds the given labelset to all metrics in the given metricFamily's.
func rewriteMetrics(labels model.LabelSet, mfs []*dto.MetricFamily) {
	// Loop through all metric families
	for _, mf := range mfs {
		// Loop through all metrics
		for _, m := range mf.Metric {
			// TODO: Can this be faster?
			// Convert the LabelPairs back to a LabelSet
			sourceSet := make(model.LabelSet, len(m.Label))
			for _, lp := range m.Label {
				if lp.Name != nil {
					sourceSet[model.LabelName(*lp.Name)] = model.LabelValue(lp.GetValue())
				}
			}
			// Merge the input set with the additional set
			outputSet := sourceSet.Merge(labels)
			// Convert the label set back to labelPairs and attach to the Metric
			outputPairs := make([]*dto.LabelPair, 0)
			for n, v := range outputSet {
				outputPairs = append(outputPairs, &dto.LabelPair{
					// Note: could probably drop the function call and just pass a pointer
					Name:  proto.String(string(n)),
					Value: proto.String(string(v)),
				})
			}
			sort.Sort(prometheus.LabelPairSorter(outputPairs))
			// Replace the metrics labels with the given output pairs
			m.Label = outputPairs
		}
	}
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
