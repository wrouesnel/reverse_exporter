package metricProxy

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
	testScript string
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

	config := config.ExecExporterConfig{
		Command: scriptPath,
		Args:    []string{"foo", "bar"},
		Exporter: config.Exporter{
			Name:      "test_exec_proxy",
			NoRewrite: false,
			Labels:    nil,
		},
	}

	return config
}

func (s *ExecProxySuite) TestExecProxy(c *C) {
	config := s.initProxyScript(c, execProxyScript)
	defer os.Remove(config.Command)

	execProxy := newExecProxy(&config)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, config.Args)
	c.Check(execProxy.commandPath, Equals, config.Command)

	ctx := context.Background()
	//tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	//defer cancelFn()
	mfs, err := execProxy.Scrape(ctx, nil)
	c.Check(err, IsNil)
	c.Check(len(mfs), Equals, execProxyScriptNumMetrics)
}

func (s *ExecProxySuite) TestExecProxyWithBrokenScript(c *C) {
	config := s.initProxyScript(c, brokenExecProxyScript)
	defer os.Remove(config.Command)

	execProxy := newExecProxy(&config)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, config.Args)
	c.Check(execProxy.commandPath, Equals, config.Command)

	ctx := context.Background()
	tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	defer cancelFn()
	mfs, err := execProxy.Scrape(tctx, nil)
	c.Check(err, Not(IsNil))
	c.Check(len(mfs), Equals, 0)
}

func (s *ExecProxySuite) TestExecProxyWithNeverendingScript(c *C) {
	config := s.initProxyScript(c, brokenStalledExecProxyScript)
	defer os.Remove(config.Command)

	cmdFile, rerr := ioutil.ReadFile(config.Command)
	c.Assert(rerr, IsNil)

	execProxy := newExecProxy(&config)
	c.Assert(execProxy, Not(IsNil))
	c.Check(execProxy.log, Not(IsNil))
	c.Check(execProxy.arguments, DeepEquals, config.Args)
	c.Check(execProxy.commandPath, Equals, config.Command)

	ctx := context.Background()
	tctx, cancelFn := context.WithTimeout(ctx, time.Second)
	defer cancelFn()
	mfs, err := execProxy.Scrape(tctx, nil)
	c.Check(err, Not(IsNil)) // scrape should time out
	c.Check(len(mfs), Equals, 0, Commentf("Got metric families: %v\nScript:\n%s", mfs, string(cmdFile)))
}
