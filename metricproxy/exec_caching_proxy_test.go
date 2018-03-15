package metricproxy

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/wrouesnel/reverse_exporter/config"
	. "gopkg.in/check.v1"
)

const timestampingExecProxyScriptNumMetrics = 1
const timestampingTestMetricName = "test_metric_time"

const timestampingExecProxyScript = `#!/bin/bash
# This script outputs the time when it is executed.
cat << EOF
test_metric_time $(date +%s)
EOF
`

type ExecCachingProxySuite struct {
}

var _ = Suite(&ExecCachingProxySuite{})

// initProxyScript sets up a dummy exec proxy config from a variable for us.
func (s *ExecCachingProxySuite) initProxyScript(c *C, script string) config.ExecCachingExporterConfig {
	f, err := ioutil.TempFile("", fmt.Sprintf("exec_caching_proxy_test_%s", c.TestName()))
	c.Assert(err, IsNil)

	scriptPath := f.Name()

	f.WriteString(script)
	f.Chmod(os.FileMode(0700)) // Make the script executable
	f.Close()

	exporterConfig := config.ExecCachingExporterConfig{
		Command:      scriptPath,
		Args:         []string{"foo", "bar"},
		ExecInterval: model.Duration(time.Second * 1),
		Exporter: config.Exporter{
			Name:      "test_exec_proxy",
			NoRewrite: false,
			Labels:    nil,
		},
	}

	return exporterConfig
}

func (s *ExecCachingProxySuite) TestExecCachingProxy(c *C) {
	exporterConfig := s.initProxyScript(c, timestampingExecProxyScript)
	defer os.Remove(exporterConfig.Command)

	execProxy := newExecCachingProxy(&exporterConfig)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, exporterConfig.Args)
	c.Check(execProxy.commandPath, Equals, exporterConfig.Command)

	ctx := context.Background()
	//tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	//defer cancelFn()

	// Collect metrics over a time-period
	mfss := make([][]*dto.MetricFamily, 0)

	// Collect two scrapes as far as possible
	mfs, err := execProxy.Scrape(ctx, nil)
	mfss = append(mfss, mfs)
	c.Check(err, IsNil)

	mfs, err = execProxy.Scrape(ctx, nil)
	mfss = append(mfss, mfs)
	c.Check(err, IsNil)

	// Then wait 1.5 seconds and collect another
	<-time.After(time.Millisecond * 1500)
	mfs, err = execProxy.Scrape(ctx, nil)
	mfss = append(mfss, mfs)
	c.Check(err, IsNil)

	// Check all metrics are correct
	timeVals := []float64{}

	for _, mfs := range mfss {
		c.Assert(len(mfs), Equals, timestampingExecProxyScriptNumMetrics)
		// Get metric names report correctly
		for _, mf := range mfs {
			c.Assert(mf.GetName(), Equals, timestampingTestMetricName)
			for _, m := range mf.Metric {
				// Collect time value
				timeVals = append(timeVals, m.Untyped.GetValue())
			}
		}
	}

	c.Assert(len(timeVals), Equals, 3, Commentf("should have 3 time values"))
	c.Assert(timeVals[0], Equals, timeVals[1], Commentf("time vals collected under interval should be identical"))
	c.Assert(timeVals[2], Not(Equals), timeVals[1], Commentf("third time val should have executed later"))
}
