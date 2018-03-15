package metricproxy

import (
	"io"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type UtilSuite struct{}

var _ = Suite(&UtilSuite{})

func (s *UtilSuite) TestBufFunctions(c *C) {
	b := getBuf()
	b.WriteString("test string\n")

	giveBuf(b)
	b = getBuf()
	str, err := b.ReadString(byte('\n'))

	c.Check(err, Equals, io.EOF)
	c.Check(len(str), Equals, 0, Commentf("Buffer from pool was not empty - got: %s", s))
}
