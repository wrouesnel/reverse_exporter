package config_test

import (
	"encoding/base64"

	"github.com/wrouesnel/poller_exporter/pkg/config"
	. "gopkg.in/check.v1"
)

type ModelsSuite struct{}

var _ = Suite(&ModelsSuite{})

func (m *ModelsSuite) TestRegexp(c *C) {
	var regex config.Regexp
	original := []byte("^This is a regex$")
	err := regex.UnmarshalText(original)
	c.Check(err, IsNil)

	recovered, err := regex.MarshalText()
	c.Check(err, IsNil)
	c.Check(string(recovered), Equals, string(original))
}

func (m *ModelsSuite) TestBytes(c *C) {
	var b config.Bytes
	// note that this string is *not* escaped in the compiler
	original := base64.StdEncoding.EncodeToString([]byte("\xee\x86M\x14nZ\x91SqPr\xbb3\x17Q\xf8"))
	err := b.UnmarshalText([]byte(original))
	c.Check(err, IsNil)
	c.Check(len(b), Equals, 16, Commentf("should have exactly 16 bytes got %v", len(b)))

	recovered, err := b.MarshalText()
	c.Check(err, IsNil)
	c.Check(string(recovered), Equals, original)
}
