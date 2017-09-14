package metricProxy

import (
	"context"
	"net/url"
	"os"

	"github.com/hashicorp/errwrap"
	"github.com/moby/moby/pkg/ioutils"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// ensure fileProxy implements MetricProxy
var _ MetricProxy = &fileProxy{}

// fileProxy implements a reverse metric proxy which simply reads a file
// of text-formatted metrics from disk (similar to the node_exporter textfile collector).
type fileProxy struct {
	filePath string
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (fp *fileProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	retMetrics := make([]*dto.MetricFamily, 0)

	metricFile, err := os.OpenFile(fp.filePath, os.O_RDONLY, os.FileMode(0777))
	if err != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, err)
	}

	// Ensure weird file behavior doesn't leave multiple open processes
	rc := ioutils.NewCancelReadCloser(ctx, metricFile)
	defer rc.Close()

	mfs, err := decodeMetrics(rc, expfmt.FmtText)
	if err != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, err)
	}

	return mfs, nil
}
