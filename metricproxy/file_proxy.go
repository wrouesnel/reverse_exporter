package metricproxy

import (
	"context"
	"net/url"
	"os"

	"github.com/docker/docker/pkg/ioutils"
	"github.com/hashicorp/errwrap"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/log"
	"github.com/wrouesnel/reverse_exporter/config"
)

// ensure fileProxy implements MetricProxy
var _ MetricProxy = &fileProxy{}

// fileProxy implements a reverse metric proxy which simply reads a file
// of text-formatted metrics from disk (similar to the node_exporter textfile collector).
type fileProxy struct {
	filePath string
	log      log.Logger
}

func newFileProxy(config *config.FileExporterConfig) *fileProxy {
	return &fileProxy{
		filePath: config.Path,
		log:      log.Base(),
	}
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (fp *fileProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	retMetrics := make([]*dto.MetricFamily, 0)

	metricFile, ferr := os.OpenFile(fp.filePath, os.O_RDONLY, os.FileMode(0777))
	if ferr != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, ferr)
	}

	// Ensure weird file behavior doesn't leave multiple open processes
	rc := ioutils.NewCancelReadCloser(ctx, metricFile)
	defer func() {
		if err := rc.Close(); err != nil {
			fp.log.With("error", err.Error()).Errorln("Error closing file")
		}
	}()

	mfs, derr := decodeMetrics(rc, expfmt.FmtText)
	if derr != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, derr)
	}

	return mfs, nil
}
