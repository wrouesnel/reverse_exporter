package metricProxy

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/wrouesnel/reverse_exporter/config"
	. "gopkg.in/check.v1"
)

const testFileMetricsLen = 1
const testFileMetricName string = "constant_file_metric"
const testFileMetricValue float64 = 100
var testFileMetrics string = fmt.Sprintf(`
# HELP constant_file_metric This is a sample metric which is a constant
# TYPE constant_file_metric gauge
%s %v
`,testFileMetricName, testFileMetricValue)

type FileProxySuite struct{}

var _ = Suite(&FileProxySuite{})

func (s *FileProxySuite) TestFileProxy(c *C) {
	tempMetrics, err := ioutil.TempFile("","file_proxy_test")
	c.Assert(err, IsNil)

	// Get the path to the test metrics
	filename := tempMetrics.Name()
	defer os.Remove(filename) // nolint: errcheck

	// Setup the file.
	tempMetrics.WriteString(testFileMetrics)
	tempMetrics.Sync()

	// Setup the test config
	config := config.FileExporterConfig{
		Path: filename,
		Exporter: config.Exporter{
			Name: "test-file-exporter",
			NoRewrite: false,
			Labels: nil,
		},
	}

	fileProxy := newFileProxy(&config)
	c.Assert(fileProxy, Not(IsNil), Commentf("newFileProxy returned nil with valid config"))

	// Check configuration was created correctly from config.
	c.Check(fileProxy.filePath, Equals, config.Path, Commentf("filepath doesn't match filename"))
	c.Check(fileProxy.log, Not(IsNil), Commentf("logger wasn't initialized to a value"))

	// Check we can scrape when file exists
	ctx := context.Background()
	mfs, err := fileProxy.Scrape(ctx, nil)
	c.Assert(err, IsNil, Commentf("could not scrape from file exporter when file exists"))
	c.Check(len(mfs), Equals, testFileMetricsLen, Commentf("didn't receive right number of metrics"))

	// Check metrics scrape correctly
	for _, mf := range mfs {
		c.Check(mf.GetName(), Equals, testFileMetricName, Commentf("read wrong metric name"))
		c.Check(mf.GetType().String(), Equals, "GAUGE", Commentf("got wrong metric type"))
		c.Check(mf.GetMetric()[0].GetGauge().GetValue(), Equals, testFileMetricValue)
	}

	// Check new metrics added to the file work
	newMetrics := `
	# HELP extra_metric This is a sample metric written after the file has been scraped
	# TYPE extra_metric counter
	extra_metric 1000
	`
	tempMetrics.WriteString(newMetrics)
	tempMetrics.Sync()

	mfs, err = fileProxy.Scrape(ctx, nil)
	c.Assert(err, IsNil, Commentf("metric scrape failed after new metrics added"))
	c.Check(len(mfs), Equals, testFileMetricsLen + 1, Commentf("didn't receive right number of metrics"))

	// Check fileProxy fails when file doesn't exist
	os.Remove(filename)
	mfs, err = fileProxy.Scrape(ctx, nil)
	c.Check(err, Not(IsNil), Commentf("no error when file does not exist?"))
	c.Check(len(mfs), Equals, 0, Commentf("got metrics but file shouldn't exist?"))
}
