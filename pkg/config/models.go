package config

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	TLSCertificatePoolMaxNonFileEntryReturn int = 50
)

const (
	TLSCACertsSystem string = "system"
	ProxyEnvironment string = "environment"
	ProxyDirect      string = "direct"
)

var (
	ErrInvalidInputType = errors.New("invalid input type for decoder")
	ErrInvalidPEMFile   = errors.New("PEM file could not be added to certificate pool")
)

// HTTPStatusRange is a range of HTTP status codes which can be specifid in YAML using human-friendly ranging notation.
type HTTPStatusRange map[int]bool

// FromString initializes a new HTTPStatusRange from the given string specifier
//nolint:cyclop
func (hsr *HTTPStatusRange) FromString(ranges string) error {
	const HTTPStatusRangeBase int = 10
	const HTTPStatusRangeBitSize int = 32
	*hsr = make(HTTPStatusRange)
	var statusCodes []int
	fields := strings.Fields(ranges)

	for _, v := range fields {
		code, err := strconv.ParseInt(v, HTTPStatusRangeBase, HTTPStatusRangeBitSize)
		if err == nil {
			statusCodes = append(statusCodes, int(code))
			continue
		}
		// Didn't work, but might be a range
		if strings.Count(v, "-") == 0 || strings.Count(v, "-") > 1 {
			return errors.New("HTTPStatusRange.FromString: not a valid range")
		}
		// Is a range.
		statusRange := strings.Split(v, "-")
		startCode, err := strconv.ParseInt(statusRange[0], HTTPStatusRangeBase, HTTPStatusRangeBitSize)
		if err != nil {
			return errors.Wrapf(err, "HTTPStatusRange.FromString failed: startCode: %s", v)
		}

		endCode, err := strconv.ParseInt(statusRange[1], HTTPStatusRangeBase, HTTPStatusRangeBitSize)
		if err != nil {
			return errors.Wrapf(err, "HTTPStatusRange.FromString failed: endCode: %s", v)
		}

		// Loop over the codes in sequential order
		if startCode < endCode {
			for i := startCode; i < endCode+1; i++ {
				statusCodes = append(statusCodes, int(i))
			}
		} else {
			for i := startCode; i > endCode-1; i-- {
				statusCodes = append(statusCodes, int(i))
			}
		}
	}

	for _, v := range statusCodes {
		(*hsr)[v] = true
	}
	return nil
}

// UnmarshalText implements the encoding.TextUnmarshaler.
func (hsr *HTTPStatusRange) UnmarshalText(text []byte) error {
	return hsr.FromString(string(text))
}

// MarshalText implements the encoding.TextMarshaler.
func (hsr HTTPStatusRange) MarshalText() ([]byte, error) {
	statusCodes := make([]int, 0, len(hsr))
	var output []string
	for k := range hsr {
		statusCodes = append(statusCodes, k)
	}

	sort.Ints(statusCodes)

	// This could probably be neater, but its what you get when you iterate.
	idx := 0
	for {
		start := statusCodes[idx]
		prev := start
		for {
			idx++
			if idx >= len(statusCodes) {
				break
			}
			if statusCodes[idx]-prev != 1 {
				// Check if it's a single number
				if statusCodes[idx-1] == start {
					output = append(output, fmt.Sprintf("%d", start))
				} else {
					output = append(output, fmt.Sprintf("%d-%d", start, statusCodes[idx-1]))
				}
				break
			}
			prev = statusCodes[idx]
		}
		if idx >= len(statusCodes) {
			break
		}
	}

	return []byte(strings.Join(output, " ")), nil
}

// HTTPVerb wraps string to ensure that verbs are uppercase. It doesn't check if they're
// valid to allow people to do stupid things with them if they want.
type HTTPVerb string

func (d HTTPVerb) String() string {
	return string(d)
}

// MarshalText implements the encoding.TextMarshaler interface.
func (d *HTTPVerb) MarshalText() ([]byte, error) {
	return []byte(*d), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (d *HTTPVerb) UnmarshalText(text []byte) error {
	*d = HTTPVerb(strings.ToUpper(string(text)))
	switch *d {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace:
		return nil
	default:
		return errors.Wrapf(ErrInvalidInputType, "HTTPVerb.UnmarshalText: invalid HTTP verb")
	}
}

// Bytes implements a custom []byte slice implemented TextMarshaller so base64
// binary content can be passed n.
type Bytes []byte

// MarshalText implements the encoding.TextMarshaler interface.
func (d *Bytes) MarshalText() ([]byte, error) {
	return []byte(base64.StdEncoding.EncodeToString(*d)), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (d *Bytes) UnmarshalText(text []byte) error {
	decoded, err := base64.StdEncoding.DecodeString(string(text))
	*d = decoded
	return errors.Wrapf(err, "Bytes.UnmarshalText: base64 decoding failed")
}

// Regexp encapsulates a regexp.Regexp and makes it YAML marshallable.
type Regexp struct {
	*regexp.Regexp
}

// MarshalText implements the encoding.TextMarshaler interface.
func (r *Regexp) MarshalText() ([]byte, error) {
	return []byte(r.Regexp.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (r *Regexp) UnmarshalText(text []byte) error {
	var err error
	r.Regexp, err = regexp.Compile(string(text))
	if err != nil {
		return errors.Wrapf(err, "UnmarshalText failed: %v", string(text))
	}
	return nil
}

// URL is a custom URL type that allows validation at configuration load time.
type URL struct {
	*url.URL
}

func NewURL(url string) (URL, error) {
	u := URL{nil}
	err := u.UnmarshalText([]byte(url))
	return u, err
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for URLs.
func (u *URL) UnmarshalText(text []byte) error {
	urlp, err := url.Parse(string(text))

	if err != nil {
		return errors.Wrap(err, "URL.UnmarshalText failed")
	}
	u.URL = urlp
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for URLs.
func (u *URL) MarshalText() ([]byte, error) {
	if u.URL != nil {
		return []byte(u.String()), nil
	}
	return []byte(""), nil
}

// TLSCertificatePool is our custom type for decoding a certificate pool out of
// YAML.
type TLSCertificatePool struct {
	*x509.CertPool
	original []string
}

// MapStructureDecode implements the yaml.Unmarshaler interface for tls_cacerts.
//nolint:funlen,cyclop
func (t *TLSCertificatePool) MapStructureDecode(input interface{}) error {
	// Get the slice
	interfaceSlice, ok := input.([]interface{})
	if !ok {
		return errors.Wrapf(ErrInvalidInputType, "expected []string got %T", input)
	}

	// Get the strings
	strErrors := lo.Map(interfaceSlice, func(t interface{}, i int) lo.Tuple2[string, bool] {
		strValue, ok := t.(string)
		return lo.T2(strValue, ok)
	})

	// Extract errors
	err := lo.Reduce[lo.Tuple2[string, bool], error](strErrors, func(r error, t lo.Tuple2[string, bool], _ int) error {
		// Return the first error we got if its there
		if r != nil {
			return r
		}
		_, ok := lo.Unpack2(t)
		if !ok {
			return errors.Wrapf(ErrInvalidInputType, "invalid input type in certificate list")
		}
		return nil
	}, nil)
	if err != nil {
		return errors.Wrapf(err, "TLSCertificatePool.MapStructureDecode")
	}

	// Flatten the valid list out to []string
	caCertSpecEntries := lo.Map(strErrors, func(t lo.Tuple2[string, bool], _ int) string {
		value, _ := lo.Unpack2(t)
		return value
	})

	// Prescan to check for system cert package request
	t.CertPool = nil
	for _, entry := range caCertSpecEntries {
		if entry == TLSCACertsSystem {
			rootCAs, err := x509.SystemCertPool()
			if err != nil {
				zap.L().Warn("could not fetch system certificate pool", zap.Error(err))
				rootCAs = x509.NewCertPool()
			}
			t.CertPool = rootCAs
			break
		}
	}

	if t.CertPool == nil {
		t.CertPool = x509.NewCertPool()
	}

	//nolint:nestif
	for idx, entry := range caCertSpecEntries {
		var pem []byte
		itemSample := ""
		if entry == TLSCACertsSystem {
			// skip - handled above
			continue
		} else if _, err := os.Stat(entry); err == nil {
			// Is a file
			pem, err = ioutil.ReadFile(entry)
			if err != nil {
				return errors.Wrapf(err, "could not read certificate file: %s", entry)
			}
			itemSample = entry
		} else {
			pem = []byte(entry)
			if len(entry) < TLSCertificatePoolMaxNonFileEntryReturn {
				itemSample = entry
			} else {
				itemSample = entry[:TLSCertificatePoolMaxNonFileEntryReturn]
			}
		}
		if ok := t.CertPool.AppendCertsFromPEM(pem); !ok {
			return errors.Wrapf(ErrInvalidPEMFile, "failed at item %v: %v", idx, itemSample)
		}
	}

	t.original = caCertSpecEntries

	return nil
}

// IPNetwork is the config wrapper type for an IP Network.
type IPNetwork struct {
	net.IPNet
}

func (ipn *IPNetwork) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return errors.Wrapf(err, "IPNetwork.UnmarshalYAML failed: %s", s)
	}

	ipn.IPNet = *ipnet
	return nil
}

func (ipn IPNetwork) MarshalYAML() (interface{}, error) {
	return ipn.String(), nil
}

// ProxyURL is a custom type to validate roxy specifications.
type ProxyURL string

// UnmarshalText implements encoding.UnmarshalText.
func (p *ProxyURL) UnmarshalText(text []byte) error {
	s := string(text)
	if _, err := url.Parse(s); err != nil {
		*p = ProxyURL(s)
		return errors.Wrapf(err, "ProxyURL UnmarshalText")
	}
	switch s {
	case ProxyDirect, ProxyEnvironment:
		*p = ProxyURL(s)
		return nil
	default:
		return nil
	}
}

// UnmarshalText MarshalText encoding.UnmarshalText.
func (p *ProxyURL) MarshalText() ([]byte, error) {
	return []byte(*p), nil
}
