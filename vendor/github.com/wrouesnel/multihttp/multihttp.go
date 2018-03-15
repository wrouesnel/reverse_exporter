package multihttp

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
	//"fmt"
	"crypto/x509"
	"errors"
	"io/ioutil"

	"github.com/hashicorp/errwrap"
)

const (
	certParam   string = "tlscert"
	keyParam    string = "tlskey"
	caCertParam string = "tlsclientca"

	networkUnix string = "unix"
	networkTCP  string = "tcp"
)

// nolint: golint
var (
	ErrErrorLoadingCertificate         = errors.New("could not load TLS certificate")
	ErrErrorMissingTLSParameters       = errors.New("TLS modes require tlscert and tlskey params")
	ErrErrorLoadingClientCACertificate = errors.New("error loading client CA certificate")
	ErrUnknownListenScheme             = errors.New("unknown listen scheme")
)

// ListenAddressConfig is the parsed form of a multihttp address
type ListenAddressConfig struct {
	// NetworkType is the type of socket connection
	NetworkType string
	// Address is either the IP or socket path.
	Address string
	// TLS config is the TLS parameters passed as part of the URL.
	TLSConfig *tls.Config
}

// ListenerError maps a listener to it's error channel.
type ListenerError struct {
	Listener net.Listener
	Error    error
}

func getNetworkTypeAndAddressFromURL(u *url.URL) (string, string) {
	var networkType, address string

	switch u.Scheme {
	case networkTCP:
		networkType = u.Scheme
		address = u.Host
	case networkUnix:
		networkType = networkUnix
		address = u.Path
	}

	return networkType, address
}

// ParseAddress parses the given address string into an expanded configuration
// struct. It is normally used by the Listen function.
func ParseAddress(address string) (ListenAddressConfig, error) {
	retAddr := ListenAddressConfig{}

	urlp, err := url.Parse(address)
	if err != nil {
		return retAddr, err
	}

	switch urlp.Scheme {
	case networkTCP, networkUnix: // tcp
		retAddr.NetworkType, retAddr.Address = getNetworkTypeAndAddressFromURL(urlp)
	case "tcps", "unixs": // tcp with tls
		urlp.Scheme = urlp.Scheme[:len(urlp.Scheme)-1]
		retAddr.NetworkType, retAddr.Address = getNetworkTypeAndAddressFromURL(urlp)

		tlsConfig := new(tls.Config)
		tlsConfig.NextProtos = []string{"http/1.1"}

		queryParams := urlp.Query()
		if queryParams == nil {
			return retAddr, ErrErrorMissingTLSParameters
		}

		// Get certificate and key path.
		certPath := queryParams.Get(certParam)
		keyPath := queryParams.Get(keyParam)

		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return retAddr, errwrap.Wrap(ErrErrorLoadingCertificate, err)
		}

		// Optional: client verification path
		if caCertPath := queryParams.Get(caCertParam); caCertPath != "" {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert

			// Require acceptable clientCAs to be explicitly specified.
			caCerts, caerr := ioutil.ReadFile(caCertPath)
			if caerr != nil {
				return retAddr, errwrap.Wrap(ErrErrorLoadingClientCACertificate, caerr)
			}

			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCerts)

			tlsConfig.ClientCAs = caCertPool
		}
		retAddr.TLSConfig = tlsConfig
	default:
		return retAddr, ErrUnknownListenScheme
	}

	return retAddr, nil
}

// CloseAndCleanUpListeners runs clean up on a list of listeners,
// namely deleting any Unix socket files
// nolint: errcheck,gas
func CloseAndCleanUpListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		listener.Close()
		addr := listener.Addr()
		switch addr.(type) {
		case *net.UnixAddr:
			os.Remove(addr.String())
		}
	}
}

// Listen is a non-blocking function to listen on multiple http sockets. Returns
// a list of the created listener interfaces. Even in the case of errors,
// successfully listening interfaces are returned to allow for clean up.
func Listen(addresses []string, handler http.Handler) ([]net.Listener, <-chan *ListenerError, error) {
	var listeners []net.Listener

	// Master error channel - all errors are propagated here. Length is set to
	// listener length so go routines will clean up even if the channel is
	// ignored.
	errCh := make(chan *ListenerError, len(addresses))

	for _, address := range addresses {
		addressConfig, aerr := ParseAddress(address)
		if aerr != nil {
			return listeners, errCh, aerr
		}

		var listener net.Listener
		var lerr error

		listener, lerr = net.Listen(addressConfig.NetworkType, addressConfig.Address)
		// Errored making listener?
		if lerr != nil {
			return listeners, errCh, lerr
		}

		// TLS connection?
		if addressConfig.TLSConfig != nil {
			listener = tls.NewListener(listener, addressConfig.TLSConfig)
		}

		// Append and start serving on listener
		listener = maybeKeepAlive(listener)
		listeners = append(listeners, listener)
		go func(listener net.Listener) {
			err := http.Serve(listener, handler)
			// Return the listener and the error it returned.
			errCh <- &ListenerError{
				Listener: listener,
				Error:    err,
			}
		}(listener)
	}

	return listeners, errCh, nil
}

// Checks if a listener is a TCP and needs a keepalive handler
func maybeKeepAlive(ln net.Listener) net.Listener {
	if o, ok := ln.(*net.TCPListener); ok {
		return &tcpKeepAliveListener{o}
	}
	return ln
}

// Irritatingly the tcpKeepAliveListener is not public, so we need to recreate it.
// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted connections.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	err = tc.SetKeepAlive(true)
	if err != nil {
		return nil, err
	}
	err = tc.SetKeepAlivePeriod(3 * time.Minute)
	if err != nil {
		return nil, err
	}
	return tc, nil
}

// Returns a dialer which ignores the address string and connects to the
// given socket always.
//func newDialer(addr string) (func (proto, addr string) (conn net.Conn, err error), error) {
//	realProtocol, realAddress, err := ParseAddress(addr)
//	if err != nil {
//		return nil, err
//	}
//
//	return func (proto, addr string) (conn net.Conn, err error) {
//		return net.Dial(realProtocol, realAddress)
//	}, nil
//}
//
//// Initialize an HTTP client which connects to the provided socket address to
//// service requests. The hostname in requests is parsed as a header only.
//func NewClient(addr string) (*http.Client, error) {
//	dialer, err := newDialer(addr)
//	if err != nil {
//		return nil, err
//	}
//
//	tr := &http.Transport{ Dial: dialer, }
//	client := &http.Client{Transport: tr}
//
//	return client, nil
//}
