package metricProxy

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"io"
)

// decodeMetrics decodes metrics from an io.Reader. Returns an empty slice on error.
// Use expfmt.Constants to pass in format. Breaks on first metric decoding error.
func decodeMetrics(reader io.Reader, format expfmt.Format) ([]*dto.MetricFamily, error) {
	var merr error
	mfs := make([]*dto.MetricFamily, 0)

	mfDec := expfmt.NewDecoder(reader, format)

	for {
		mf := &dto.MetricFamily{}
		if err := mfDec.Decode(mf); err != nil {
			if err != io.EOF {
				merr = err
			}
			break
		}
		mfs = append(mfs, mf)
	}

	return mfs, merr
}
