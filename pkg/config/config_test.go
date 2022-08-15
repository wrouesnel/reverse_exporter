package config_test

import (
	"testing"

	"github.com/wrouesnel/reverse_exporter/pkg/config"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type ConfigSuite struct{}

var _ = Suite(&ConfigSuite{})

//func structDiff(a, b interface{}) string {
//	diff := difflib.UnifiedDiff{
//		A:        difflib.SplitLines(spew.Sdump(a)),
//		B:        difflib.SplitLines(spew.Sdump(b)),
//		FromFile: "a",
//		ToFile:   "b",
//		Context:  3,
//	}
//	text, _ := difflib.GetUnifiedDiffString(diff)
//	return text
//}

func (s *ConfigSuite) TestConfigParsing(c *C) {
	cfg, err := config.LoadFromFile("test_data/test_config.yml")
	c.Assert(err, IsNil, Commentf(func(e error) string {
		if err != nil {
			return err.Error()
		}
		return ""
	}(err)))
	c.Assert(cfg.Web, Not(IsNil))

	c.Assert(cfg.ReverseExporters, Not(IsNil))
}
