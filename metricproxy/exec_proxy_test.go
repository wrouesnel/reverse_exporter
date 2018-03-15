package metricproxy

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/wrouesnel/reverse_exporter/config"
	. "gopkg.in/check.v1"
)

const execProxyScriptNumMetrics = 2
const execProxyScript = `#!/bin/bash
cat << EOF
test_metric_one{arg1="$1",arg2="$2"} 54321
test_metric_two{arg1="$1",arg2="$2"} 12345
EOF
`

var _ = Suite(&ExecProxySuite{})

const brokenExecProxyScript = `#!/bin/bash
exit 1
`

const brokenStalledExecProxyScript = `#!/bin/bash
# This script goes into a loop and never returns anything.
while [ 1 ]; do
	sleep 1
done
`

type ExecProxySuite struct {
}

var _ = Suite(&ExecProxySuite{})

// initProxyScript sets up a dummy exec proxy config from a variable for us.
func (s *ExecProxySuite) initProxyScript(c *C, script string) config.ExecExporterConfig {
	f, err := ioutil.TempFile("", fmt.Sprintf("exec_proxy_test_%s", c.TestName()))
	c.Assert(err, IsNil)

	scriptPath := f.Name()

	f.WriteString(script)
	f.Chmod(os.FileMode(0700)) // Make the script executable
	f.Close()

	exporterConfig := config.ExecExporterConfig{
		Command: scriptPath,
		Args:    []string{"foo", "bar"},
		Exporter: config.Exporter{
			Name:      "test_exec_proxy",
			NoRewrite: false,
			Labels:    nil,
		},
	}

	return exporterConfig
}

func (s *ExecProxySuite) TestExecProxy(c *C) {
	exporterConfig := s.initProxyScript(c, execProxyScript)
	defer os.Remove(exporterConfig.Command)

	execProxy := newExecProxy(&exporterConfig)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, exporterConfig.Args)
	c.Check(execProxy.commandPath, Equals, exporterConfig.Command)

	ctx := context.Background()
	//tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	//defer cancelFn()
	mfs, err := execProxy.Scrape(ctx, nil)
	c.Check(err, IsNil)
	c.Check(len(mfs), Equals, execProxyScriptNumMetrics)
}

func (s *ExecProxySuite) TestExecProxyWithBrokenScript(c *C) {
	exporterConfig := s.initProxyScript(c, brokenExecProxyScript)
	defer os.Remove(exporterConfig.Command)

	execProxy := newExecProxy(&exporterConfig)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, exporterConfig.Args)
	c.Check(execProxy.commandPath, Equals, exporterConfig.Command)

	ctx := context.Background()
	tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	defer cancelFn()
	mfs, err := execProxy.Scrape(tctx, nil)
	c.Check(err, Not(IsNil))
	c.Check(len(mfs), Equals, 0)
}

func (s *ExecProxySuite) TestExecProxyWithNeverendingScript(c *C) {
	exporterConfig := s.initProxyScript(c, brokenStalledExecProxyScript)
	defer os.Remove(exporterConfig.Command)

	cmdFile, rerr := ioutil.ReadFile(exporterConfig.Command)
	c.Assert(rerr, IsNil)

	execProxy := newExecProxy(&exporterConfig)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, exporterConfig.Args)
	c.Check(execProxy.commandPath, Equals, exporterConfig.Command)

	ctx := context.Background()
	tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	defer cancelFn()
	mfs, err := execProxy.Scrape(tctx, nil)
	c.Check(err, Not(IsNil)) // scrape should time out and not get data
	c.Check(len(mfs), Equals, 0, Commentf("Got metric families: %v\nScript:\n%s", mfs, string(cmdFile)))
}

func (s *ExecProxySuite) TestExecProxyQueuesCorrectly(c *C) {
	exporterConfig := s.initProxyScript(c, brokenStalledExecProxyScript)
	defer os.Remove(exporterConfig.Command)

	cmdFile, rerr := ioutil.ReadFile(exporterConfig.Command)
	c.Assert(rerr, IsNil)

	execProxy := newExecProxy(&exporterConfig)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, exporterConfig.Args)
	c.Check(execProxy.commandPath, Equals, exporterConfig.Command)

	// Make a bunch of contexts
	//ctxs := []context.Context{}
	cFns := []context.CancelFunc{}
	doneChs := []chan struct{}{}

	for i := 0; i < 10; i++ {
		ctx := context.Background()
		tctx, cancelFn := context.WithCancel(ctx)
		//ctxs = append(ctxs, tctx)
		cFns = append(cFns, cancelFn)
		doneCh := make(chan struct{})
		doneChs = append(doneChs, doneCh)
		// Invoke scrapes
		go func(ctx context.Context, thisDoneCh chan struct{}) {
			mfs, err := execProxy.Scrape(ctx, nil)
			c.Check(err, Not(IsNil)) // scrape should time out and not get data
			c.Check(len(mfs), Equals, 0, Commentf("Got metric families: %v\nScript:\n%s", mfs, string(cmdFile)))
			close(thisDoneCh)
		}(tctx, doneCh)
	}

	// Kill scrapes 1 by 1 - if things are working correctly, we shouldn't have multiple scrapes error at
	// once since the others are still alive.
	for i := 0; i < 10; i++ {
		cFns[i]()
		// Check the channel we expect to close is closed
		<-doneChs[i]
		// Check all the other channels are not
		for k := i + 1; k < 10; k++ {
			select {
			case <-doneChs[k]:
				c.Errorf("Other channels exited when one scrape was cancelled")
			default:
				c.Logf("Channel %d correctly still active after %d was cancelled", k, i)
				continue
			}
		}
	}
}
