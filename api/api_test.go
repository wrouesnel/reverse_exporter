package api

import (
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type APISuite struct{}

var _ = Suite(&APISuite{})

func (s *APISuite) TestAPICreation(c *C) {
	settings := apisettings.APISettings{
		ContextPath: "/some/type/of/path",
	}

	router := httprouter.New()
	router = NewAPIv1(settings, router)

	// Kind of a boring check, but important
	c.Check(router, Not(IsNil))
}
