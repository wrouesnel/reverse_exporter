package metricproxy

import (
	"context"
	"net/url"
	"os"

	"github.com/wrouesnel/reverse_exporter/pkg/config"
	"go.uber.org/zap"

	"github.com/hashicorp/errwrap"
	"github.com/moby/moby/pkg/ioutils"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// ensure fileProxy implements MetricProxy.
var _ MetricProxy = &fileProxy{}

// fileProxy implements a reverse metric proxy which simply reads a file
// of text-formatted metrics from disk (similar to the node_exporter textfile collector).
type fileProxy struct {
	filePath string
	log      *zap.Logger
}

func newFileProxy(config *config.FileExporterConfig) *fileProxy {
	return &fileProxy{
		filePath: config.Path,
		log:      zap.L(),
	}
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (fp *fileProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	retMetrics := make([]*dto.MetricFamily, 0)

	metricFile, ferr := os.Open(fp.filePath)
	if ferr != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, ferr)
	}

	// Ensure weird file behavior doesn't leave multiple open processes
	readCloser := ioutils.NewCancelReadCloser(ctx, metricFile)
	defer func() {
		if err := readCloser.Close(); err != nil {
			fp.log.Error("Error closing file", zap.Error(err))
		}
	}()

	mfs, derr := decodeMetrics(readCloser, expfmt.FmtText)
	if derr != nil {
		return retMetrics, errwrap.Wrap(ErrFileProxyScrapeError, derr)
	}

	return mfs, nil
}
