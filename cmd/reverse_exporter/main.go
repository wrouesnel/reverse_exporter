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
	"time"
	"sync"
)

type AppConfig struct {
	ConfigFile  string
	MetricsPath string

	ContextPath string
	StaticProxy string

	ListenAddrs string
}

func main() {
	appConfig := AppConfig{}

	flag.StringVar(&appConfig.ConfigFile, "config.file", "reverse_exporter.yml", "Path to configuration file")
	flag.StringVar(&appConfig.MetricsPath, "metrics.path", "/metrics", "URL path to expose metrics under")

	flag.StringVar(&appConfig.ContextPath, "http.context-path", "", "Context-path of any additional reverse proxy")
	flag.StringVar(&appConfig.StaticProxy, "http.static-proxy", "", "Static Assets proxy server path")

	flag.StringVar(&appConfig.ListenAddrs, "http.listen-addrs", "tcp://0.0.0.0:9998", "Comma-separated list of listen addresses")

	// TODO: basic auth, SSL client/server support

	flag.Parse()

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

	// Setup metric reverse proxies
	reverseProxies := make([]*metricProxy.MetricReverseProxy, 0)
	for _, p := range reverseConfig.ReverseExporters {
		log.Debugln("Initializing reverse proxy for:", p.Name, p.Address)
		newProxy := metricProxy.NewMetricReverseProxy(time.Duration(p.Deadline), p.Address, p.Name)
		reverseProxies = append(reverseProxies, newProxy)
	}

	// Setup the web UI
	router := httprouter.New()
	assets.StaticFiles(apiConfig, router)

	// The goal of the reverse proxy is to make all metrics appear to come from a single instance i.e. an appliance
	// so the `/metrics` path always explicitely proxies everything.
	router.GET(appConfig.MetricsPath, func(wr http.ResponseWriter, rd *http.Request, _ httprouter.Params) {
		// TODO: generate additional metrics about each reverse proxy config


		// As an appliance, we return nothing till we know the result of our reverse proxied metrics.
		wg := new(sync.WaitGroup)
		// This channel is guarded by wg - it will be closed (and thus mutation of
		mfsCh := make(chan []*dto.MetricFamily)
		mfsResultCh := make(chan []*dto.MetricFamily)

		for _, rp := range reverseProxies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mfs, err := rp.Scrape(rd.Context())
				if err != nil {
					log.Errorln("error while proxying metric:", err)
					return	// emit nothing to the metric channel
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
		allMfs := <- mfsResultCh

		// serialize the resultant families
		metricProxy.HandleSerializeMetrics(wr, rd, allMfs)
	})

	log.Infoln("Starting web interface")
	listenAddrs := strings.Split(appConfig.ListenAddrs, ",")

	listeners, err := multihttp.Listen(listenAddrs, router)
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

	// Setup signal wait for shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdownCh
	log.Infoln("Terminating on signal:", sig)
}
