package metricProxy

import (
	"io"
	"sort"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
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
