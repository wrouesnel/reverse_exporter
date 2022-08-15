package metricproxy

import (
	"io"
	"sort"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/wrouesnel/reverse_exporter/pkg/promutil"

	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// decodeMetrics decodes metrics from an io.Reader. Returns an empty slice on error.
// Use expfmt.Constants to pass in format. Breaks on first metric decoding error.
func decodeMetrics(reader io.Reader, format expfmt.Format) ([]*dto.MetricFamily, error) {
	var merr error
	mfs := make([]*dto.MetricFamily, 0)

	mfDec := expfmt.NewDecoder(reader, format)

	for {
		metricFamily := &dto.MetricFamily{}
		if err := mfDec.Decode(metricFamily); err != nil {
			if errors.Is(err, io.EOF) {
				merr = err
			}
			break
		}
		mfs = append(mfs, metricFamily)
	}

	return mfs, merr
}

// rewriteMetrics adds the given labelset to all metrics in the given metricFamily's.
func rewriteMetrics(labels model.LabelSet, mfs []*dto.MetricFamily) {
	// Loop through all metric families
	for _, mf := range mfs {
		// Loop through all metrics
		for _, metric := range mf.Metric {
			// Convert the LabelPairs back to a LabelSet
			sourceSet := make(model.LabelSet, len(metric.Label))
			for _, lp := range metric.Label {
				if lp.Name != nil {
					sourceSet[model.LabelName(*lp.Name)] = model.LabelValue(lp.GetValue())
				}
			}
			// Merge the input set with the additional set
			outputSet := sourceSet.Merge(labels)
			// Convert the label set back to labelPairs and attach to the Metric
			outputPairs := make([]*dto.LabelPair, 0)
			for n, v := range outputSet {
				name := new(string)
				value := new(string)
				*name = string(n)
				*value = string(v)
				outputPairs = append(outputPairs, &dto.LabelPair{
					Name:  name,
					Value: value,
				})
			}
			sort.Sort(promutil.LabelPairSorter(outputPairs))
			// Replace the metrics labels with the given output pairs
			metric.Label = outputPairs
		}
	}
}

// handleSerializeMetrics writes the samples as metrics to the given http.ResponseWriter.
func handleSerializeMetrics(w http.ResponseWriter, req *http.Request, mfs []*dto.MetricFamily) {
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
	if _, err := w.Write(buf.Bytes()); err != nil {
		zap.L().Debug("Error writing to requestor", zap.Error(err))
	}
}

// decorateWriter wraps a writer to handle gzip compression if requested.  It
// returns the decorated writer and the appropriate "Content-Encoding" header
// (which is empty if no compression is enabled).
func decorateWriter(request *http.Request, writer io.Writer) (io.Writer, string) {
	header := request.Header.Get(acceptEncodingHeader)
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
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
	ret, ok := buf.(*bytes.Buffer)
	if !ok {
		panic("BUG: did not get *bytes.Buffer from buf")
	}
	return ret
}

func giveBuf(buf *bytes.Buffer) {
	buf.Reset()
	bufPool.Put(buf)
}
