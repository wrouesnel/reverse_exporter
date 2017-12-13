package main

import (
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/common/log"
	"github.com/wrouesnel/multihttp"

	"github.com/wrouesnel/reverse_exporter/api"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"
	"github.com/wrouesnel/reverse_exporter/config"
	"github.com/wrouesnel/reverse_exporter/metricProxy"
	"github.com/wrouesnel/reverse_exporter/version"
	"gopkg.in/alecthomas/kingpin.v2"
)

// AppConfig represents the total command line application configuration which is
// applied at startup.
type AppConfig struct {
	ConfigFile string
	//MetricsPath string

	ContextPath string
	StaticProxy string

	ListenAddrs string

	PrintVersion bool
}

func realMain(appConfig AppConfig) int {
	apiConfig := apisettings.APISettings{}
	apiConfig.ContextPath = appConfig.ContextPath

	if appConfig.ConfigFile == "" {
		log.Errorln("No app config specified.")
		return 1
	}

	reverseConfig, err := config.LoadFromFile(appConfig.ConfigFile)
	if err != nil {
		log.Errorln("Could not parse configuration file:", err)
		return 1
	}

	// Setup the web UI
	router := httprouter.New()
	router = api.NewAPIv1(apiConfig, router)

	log.Debugln("Begin initializing reverse proxy backends")
	initializedPaths := make(map[string]http.Handler)
	for _, rp := range reverseConfig.ReverseExporters {
		if rp.Path == "" {
			log.Errorln("Blank exporter paths are not allowed.")
			return 1
		}

		if _, found := initializedPaths[rp.Path]; found {
			log.Errorln("Exporter paths must be unique. %s already exists.", rp.Path)
			return 1
		}

		proxyHandler, perr := metricProxy.NewMetricReverseProxy(rp)
		if perr != nil {
			log.Errorln("Error initializing reverse proxy for path:", rp.Path)
			return 1
		}

		router.Handler("GET", apiConfig.WrapPath(rp.Path), proxyHandler)

		initializedPaths[rp.Path] = proxyHandler
	}
	log.Debugln("Finished initializing reverse proxy backends")
	log.With("num_reverse_endpoints", len(reverseConfig.ReverseExporters)).Infoln("Initialized backends")

	log.Infoln("Starting HTTP server")
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
		log.Errorln("Startup failed for a listener:", err)
		return 1
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
		return 0
	case listenerErr := <-listenerErrs:
		log.Errorln("Terminating due to listener shutdown:", listenerErr.Error)
		return 1
	}
}

func main() {
	appConfig := AppConfig{}

	app := kingpin.New("reverse_exporter", "Logical-decoding Prometheus exporter reverse proxy")

	app.Flag("config.file", "Path to the configuration file").
		Default("reverse_exporter.yml").StringVar(&appConfig.ConfigFile)
	app.Flag("http.context-path", "Context-path to be globally applied to the configured proxies").
		StringVar(&appConfig.ContextPath)
	app.Flag("http.listen-addrs", "Comma-separated list of listen address configurations").
		Default("tcp://0.0.0.0:9998").StringVar(&appConfig.ListenAddrs)
	app.Version(version.Version)

	log.AddFlags(app)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	os.Exit(realMain(appConfig))
}
