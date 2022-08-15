package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/samber/lo"
	"github.com/wrouesnel/reverse_exporter/pkg/config"
	"github.com/wrouesnel/reverse_exporter/pkg/metricproxy"
	"github.com/wrouesnel/reverse_exporter/version"
	"go.uber.org/zap/zapcore"

	"github.com/julienschmidt/httprouter"
	"github.com/wrouesnel/multihttp"

	"github.com/alecthomas/kong"
	"github.com/wrouesnel/reverse_exporter/api"
	"github.com/wrouesnel/reverse_exporter/api/apisettings"

	"go.uber.org/zap"
)

//nolint:gochecknoglobals
var CLI struct {
	Version   kong.VersionFlag `help:"Show version number"`
	LogLevel  string           `help:"Logging Level" enum:"debug,info,warning,error" default:"info"`
	LogFormat string           `help:"Logging format" enum:"console,json" default:"console"`

	ConfigFile string `help:"File to load poller config from" default:"reverse_exporter.yml"`
}

func main() {
	os.Exit(cmdMain(os.Args[1:]))
}

func cmdMain(args []string) int {
	vars := kong.Vars{}
	vars["version"] = version.Version
	kongParser, err := kong.New(&CLI, vars)
	if err != nil {
		panic(err)
	}

	_, err = kongParser.Parse(args)
	kongParser.FatalIfErrorf(err)

	// Configure logging
	logConfig := zap.NewProductionConfig()
	logConfig.Encoding = CLI.LogFormat
	var logLevel zapcore.Level
	if err := logLevel.UnmarshalText([]byte(CLI.LogLevel)); err != nil {
		panic(err)
	}
	logConfig.Level = zap.NewAtomicLevelAt(logLevel)

	log, err := logConfig.Build()
	if err != nil {
		panic(err)
	}

	// Replace the global logger to enable logging
	zap.ReplaceGlobals(log)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	ctx, cancelFn := context.WithCancel(context.Background())
	go func() {
		sig := <-sigCh
		log.Info("Caught signal - exiting", zap.String("signal", sig.String()))
		cancelFn()
	}()

	appLog := log.With(zap.String("config_file", CLI.ConfigFile))

	cfg, err := config.LoadFromFile(CLI.ConfigFile)
	if err != nil {
		log.Error("Could not parse configuration file:", zap.Error(err))
		return 1
	}

	return realMain(ctx, appLog, cfg)
}

func realMain(ctx context.Context, l *zap.Logger, cfg *config.Config) int {
	if cfg == nil {
		l.Error("No config specified - shutting down")
		return 1
	}

	apiConfig := apisettings.APISettings{}
	apiConfig.ContextPath = cfg.Web.ContextPath

	// Setup the web UI
	router := httprouter.New()
	router = api.NewAPIv1(apiConfig, router)

	l.Debug("Begin initializing reverse proxy backends")
	initializedPaths := make(map[string]http.Handler)
	for _, reverseExporterConfig := range cfg.ReverseExporters {
		reLog := l.With(zap.String("path", reverseExporterConfig.Path))
		if reverseExporterConfig.Path == "" {
			reLog.Error("Blank exporter paths are not allowed.")
			return 1
		}

		if _, found := initializedPaths[reverseExporterConfig.Path]; found {
			reLog.Error("Exporter paths must be unique. %s already exists.")
			return 1
		}

		proxyHandler, perr := metricproxy.NewMetricReverseProxy(reverseExporterConfig)
		if perr != nil {
			reLog.Error("Error initializing reverse proxy for path")
			return 1
		}

		router.Handler("GET", apiConfig.WrapPath(reverseExporterConfig.Path), proxyHandler)

		initializedPaths[reverseExporterConfig.Path] = proxyHandler
	}
	l.Debug("Finished initializing reverse proxy backends")
	l.Info("Initializsed backends")

	l.Info("Starting HTTP server")
	webCtx, webCancel := context.WithCancel(ctx)
	listeners, errCh, listenerErr := multihttp.Listen(lo.Map(cfg.Web.Listen, func(t config.URL, _ int) string {
		return t.String()
	}), router)
	if listenerErr != nil {
		l.Error("Error setting up listeners", zap.Error(listenerErr))
		webCancel()
	}

	// Log errors from the listener
	go func() {
		listenerErrInfo := <-errCh
		// On the first error, cancel the webCtx to shutdown
		webCancel()
		for {
			l.Error("Error from listener",
				zap.Error(listenerErrInfo.Error),
				zap.String("listener_addr", listenerErrInfo.Listener.Addr().String()))
			// Keep receiving the rest of the errors so we can log them
			listenerErrInfo = <-errCh
		}
	}()
	<-webCtx.Done()
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			l.Warn("Error closing listener during shutdown", zap.Error(err))
		}
	}

	l.Info("Exiting")
	return 0
}
