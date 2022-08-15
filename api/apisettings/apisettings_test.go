package apisettings_test

import (
	"testing"

	"github.com/wrouesnel/reverse_exporter/api/apisettings"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type APISuite struct{}

var _ = Suite(&APISuite{})

func (s *APISuite) TestPathWrapping(c *C) {
	settings := apisettings.APISettings{
		ContextPath: "/some/type/of/path",
	}

	c.Check(settings.WrapPath("/actualpath"), Equals, "/some/type/of/path/actualpath")
}
