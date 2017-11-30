package main

import (
	"flag"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/go.log"
	"github.com/wrouesnel/multihttp"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
	"github.com/wrouesnel/reverse_exporter/assets"
	"github.com/wrouesnel/reverse_exporter/config"
	"github.com/wrouesnel/reverse_exporter/metricProxy"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"sync"
	"time"

	"fmt"
	auth "github.com/abbot/go-http-auth"
	"github.com/wrouesnel/reverse_exporter/version"

	"errors"
)

type AppConfig struct {
	ConfigFile string
	//MetricsPath string

	ContextPath string
	StaticProxy string

	ListenAddrs string

	PrintVersion bool
}

func main() {
	appConfig := AppConfig{}

	flag.StringVar(&appConfig.ConfigFile, "config.file", "reverse_exporter.yml", "Path to configuration file")
	//flag.StringVar(&appConfig.MetricsPath, "metrics.path", "/metrics", "URL path to expose metrics under")

	flag.StringVar(&appConfig.ContextPath, "http.context-path", "", "Context-path of any additional reverse proxy")
	flag.StringVar(&appConfig.StaticProxy, "http.static-proxy", "", "Static Assets proxy server path")

	flag.StringVar(&appConfig.ListenAddrs, "http.listen-addrs", "tcp://0.0.0.0:9998", "Comma-separated list of listen addresses")

	flag.BoolVar(&appConfig.PrintVersion, "version", false, "Print version and exit.")

	// TODO: basic auth, SSL client/server support

	flag.Parse()

	// Print version and exit.
	if appConfig.PrintVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}

	apiConfig := apisettings.APISettings{}
	apiConfig.ContextPath = appConfig.ContextPath

	if appConfig.StaticProxy != "" {
		staticProxyUrl, err := url.Parse(appConfig.StaticProxy)
		if err != nil {
			log.Fatalln("Could not parse http.static-proxy URL:", err)
		}
		apiConfig.StaticProxy = staticProxyUrl
	}

	if appConfig.ConfigFile == "" {
		log.Fatalln("No app config specified.")
	}

	reverseConfig, err := config.LoadFromFile(appConfig.ConfigFile)
	if err != nil {
		log.Fatalln("Could not parse configuration file:", err)
	}

	// Setup the web UI
	router := httprouter.New()
	assets.StaticFiles(apiConfig, router)

	initializedPaths := make(map[string]interface{})
	for _, rp := range reverseConfig.ReversedExporters {
		if rp.Path == "" {
			log.Fatalln("Blank exporter paths are not allowed.")
		}

		if _, found := initializedPaths[rp.Path]; found {
			log.Fatalf("Exporter paths must be unique. %s already exists.", rp.Path)
		}

		proxyHandler, perr := reverseProxy(rp)
		if perr != nil {
			log.Fatalln("Error initializing reverse proxy for path:", rp.Path)
		}

		router.HandlerFunc("GET", rp.Path, proxyHandler)

		// future: store a reference to the actual handler when its an object
		initializedPaths[rp.Path] = nil
	}

	log.Infoln("Starting web interface")
	listenAddrs := strings.Split(appConfig.ListenAddrs, ",")

	listeners, listenerErrs, err := multihttp.Listen(listenAddrs, router)
	defer func() {
		for _, l := range listeners {
			if cerr := l.Close(); cerr != nil {
				log.Errorln("Error while closing listeners (ignored):", cerr)
			}
		}
	}()
	if err != nil {
		log.Panicln("Startup failed for a listener:", err)
	}
	for _, addr := range listenAddrs {
		log.Infoln("Listening on", addr)
	}

	// Setup handlers to catch the listener termination statuses.
	go func() {
		for listenerErr := range listenerErrs {
			log.Errorln("Listener Error:", listenerErr.Error)
		}
	}()

	// Setup signal wait for shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	// If a listener fails while it's listening, we'd like to panic and shutdown
	// since it shouldn't really happen.
	select {
	case sig := <-shutdownCh:
		log.Infoln("Terminating on signal:", sig)
	case listenerErr := <- listenerErrs:
		log.Errorln("Terminating due to listener shutdown:", listenerErr.Error)
	}
}

// reverseProxy returns a function which handles calling and combining multiple
// exporters in parallel. Returns an error if the configuration is invalid.
func reverseProxy(rp config.ReverseExporter) (http.HandlerFunc, error) {
	// Setup reverse proxy instances for this endpoint
	// Setup metric reverse proxies
	log := log.With("path", rp.Path)

	reverseProxies := make([]*metricProxy.MetricReverseProxy, 0)
	initializedNames := make(map[string]interface{})
	for _, ep := range rp.Exporters {
		if _, found := initializedNames[ep.Name]; found {
			log.Errorln("Exporter names must be unique within an endpoint:", ep.Name)
			return nil, errors.New("duplicate exporter name found")
		}
		log.Debugln("Initializing reverse proxy for:", ep.Name, ep.Address)
		newProxy, merr := metricProxy.NewMetricReverseProxy(time.Duration(ep.Deadline), ep.Address, ep.Name, ep.Labels)
		if merr != nil {
			log.Errorln("Error initializing metric proxy:", merr)
			return nil, errors.New("Error initializing reverse proxy")
		}
		reverseProxies = append(reverseProxies, newProxy)
	}

	// Setup the proxy endpoint handler function.
	proxyHandler := http.HandlerFunc(func(wr http.ResponseWriter, req *http.Request) {
		// As an appliance, we return nothing till we know the result of our reverse proxied metrics.
		wg := new(sync.WaitGroup)
		// This channel is guarded by wg - it will be closed when the waitgroup
		// finishes.
		mfsCh := make(chan []*dto.MetricFamily)
		mfsResultCh := make(chan []*dto.MetricFamily)

		// On request, request all included exporters to return values.
		for _, rp := range reverseProxies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mfs, err := rp.Scrape(req.Context(), req.URL.Query())
				if err != nil {
					log.Errorln("error while proxying metric:", err)
					return // emit nothing to the metric channel
				}
				mfsCh <- mfs // emit the metric families to the aggregator
			}()
			log.Debugln("Scraping reverse exporter:", rp.Name())
		}

		// metric aggregator emits result to mfsResultCh
		go func() {
			mfs := []*dto.MetricFamily{}

			for inpMfs := range mfsCh {
				mfs = append(mfs, inpMfs...)
			}

			mfsResultCh <- mfs
			close(mfsResultCh)
		}()
		// Wait for all scrapers to return and results to aggregate
		wg.Wait()
		close(mfsCh)
		// collect results from mfsResultCh
		allMfs := <-mfsResultCh

		// serialize the resultant families
		metricProxy.HandleSerializeMetrics(wr, req, allMfs)
	})

	// Add authentication to the handler
	switch rp.AuthType {
	case config.AuthTypeNone:
		log.Debugln("No authentication for endpoint.")
	case config.AuthTypeBasic:
		log.Debugln("Adding basic authentication to proxy endpoint")
		if rp.HtPasswdFile == "" {
			return nil, errors.New("no htpasswd file specified for basic auth")
		}
		secretProvider := auth.HtpasswdFileProvider(rp.HtPasswdFile)
		authenticator := auth.NewBasicAuthenticator("reverse_exporter", secretProvider)
		proxyHandler = authenticator.Wrap(auth.AuthenticatedHandlerFunc(proxyHandler))
	default:
		log.Debugln("Invalid auth type:", rp.AuthType)
		return nil, errors.New("invalid auth type")
	}

	return proxyHandler, nil
}
