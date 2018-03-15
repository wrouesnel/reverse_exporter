package config

import (
	"testing"

	"io/ioutil"
	"os"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/prometheus/common/model"
	. "gopkg.in/check.v1"
)

// This value needs to match testconfig beflow
const numEndpoints = 4

var testConfig = `
reverse_exporters:
- path: /metrics
  auth_type: basic
  htpasswd_file: example.htpasswd
  exporters:
  - http:
      name: prometheus
      address: http://127.0.0.1:9090/metrics
      timeout: 5s
  - http:
      address: http://127.0.0.1:9100/metrics
      name: node_exporter
      labels:
        node_uuid: some.special.identifier

- path: /blackbox
  exporters:
  - http:
      name: blackbox_exporter
      address: http://127.0.0.1:9115/probe
      no_rewrite: true

- path: /file-endpoint
  exporters:
  - file:
      name: cron_metrics
      path: example.metrics.prom

- path: /dynamic_endpoint
  exporters:
  - exec:
      name: dynamic_metrics
      command: ./scripted_metrics.sh
      args: ["arg1", "arg2"]
  - exec-cached:
      name: periodic_dynamic_metrics
      command: ./scripted_metrics.sh
      args: []
      # interval to execute the script over
      exec_interval: 30s
`

var expectedExporters = []ReverseExporter{
	{
		Path:         "/metrics",
		AuthType:     AuthTypeBasic,
		HtPasswdFile: "example.htpasswd",
		Exporters: []interface{}{
			HTTPExporterConfig{
				Address: "http://127.0.0.1:9090/metrics",
				Timeout: model.Duration(time.Second * 5),
				Exporter: Exporter{
					Name:      "prometheus",
					NoRewrite: false,
					Labels:    nil,
				},
			},
			HTTPExporterConfig{
				Address: "http://127.0.0.1:9100/metrics",
				Timeout: DefaultNetTimeout,
				Exporter: Exporter{
					Name:      "node_exporter",
					NoRewrite: false,
					Labels: map[string]string{
						"node_uuid": "some.special.identifier",
					},
				},
			},
		},
	},
	{
		Path:         "/blackbox",
		AuthType:     AuthTypeNone,
		HtPasswdFile: "",
		Exporters: []interface{}{
			HTTPExporterConfig{
				Address: "http://127.0.0.1:9115/probe",
				Timeout: DefaultNetTimeout,
				Exporter: Exporter{
					Name:      "blackbox_exporter",
					NoRewrite: true,
					Labels:    nil,
				},
			},
		},
	},
	{
		Path:         "/file-endpoint",
		AuthType:     AuthTypeNone,
		HtPasswdFile: "",
		Exporters: []interface{}{
			FileExporterConfig{
				Path: "example.metrics.prom",
				Exporter: Exporter{
					Name:      "cron_metrics",
					NoRewrite: false,
				},
			},
		},
	},
	{
		Path:         "/dynamic_endpoint",
		AuthType:     AuthTypeNone,
		HtPasswdFile: "",
		Exporters: []interface{}{
			ExecExporterConfig{
				Command: "./scripted_metrics.sh",
				Args:    []string{"arg1", "arg2"},
				Exporter: Exporter{
					Name: "dynamic_metrics",
				},
			},
			ExecCachingExporterConfig{
				Command:      "./scripted_metrics.sh",
				Args:         []string{},
				ExecInterval: model.Duration(time.Second * 30),
				Exporter: Exporter{
					Name: "periodic_dynamic_metrics",
				},
			},
		},
	},
}

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type ConfigSuite struct{}

var _ = Suite(&ConfigSuite{})

func structDiff(a, b interface{}) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(spew.Sdump(a)),
		B:        difflib.SplitLines(spew.Sdump(b)),
		FromFile: "a",
		ToFile:   "b",
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(diff)
	return text
}

func (s *ConfigSuite) TestConfigParsing(c *C) {
	f, err := ioutil.TempFile("", "reverse_exporter_test")

	c.Assert(err, IsNil, Commentf("error writing temporary file for config test"))

	f.WriteString(testConfig)
	f.Close()
	configFileName := f.Name()
	defer os.Remove(f.Name())

	config, err := LoadFromFile(configFileName)

	c.Assert(err, IsNil, Commentf("got error while parsing test YAML"))
	c.Assert(config, Not(IsNil), Commentf("no config returned from YAML parser"))

	c.Check(len(config.XXX), Equals, 0, Commentf("test config with no extra keys had extra keys?"))
	c.Check(len(config.ReverseExporters), Equals, numEndpoints, Commentf("test config read incorrect number of endpoints"))

	c.Check(config.ReverseExporters, DeepEquals, expectedExporters,
		Commentf("Parsed config did not match expected config.\nDifference Was:\n%s",
			structDiff(config.ReverseExporters, expectedExporters)))
}
