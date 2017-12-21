package metricProxy

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/wrouesnel/reverse_exporter/config"

	log "github.com/prometheus/common/log"
)

// ensure fileProxy implements MetricProxy
var _ MetricProxy = &execProxy{}

var (
	// ErrScrapeTimeoutBeforeExecFinished returned when a context times out before the exec exporter receives metrics
	ErrScrapeTimeoutBeforeExecFinished = errors.New("scrape timed out before exec finished")
)

// scrapeResult is used to communicate the result of a scrape to waiting listeners.
// Since scrapes can fail, it includes an error to allow scrapers to definitely
// detect errors without waiting for timeouts.
type execProxyScrapeResult struct {
	mfs []*dto.MetricFamily
	err error
}

// execProxy implements an efficient script metric proxy which aggregates scrapes.
type execProxy struct {
	commandPath string
	arguments   []string
	// execRequestMtx protects execRequest when it is being updated
	execRequestMtx *sync.Mutex
	// execRequest is closed and replaced by an incoming scrape to request results
	execRequest chan struct{}
	// execResultWg protects execResult when it is being updated
	execResultWg *sync.WaitGroup
	// execResult is closed and replaced once results from an execution are available
	execResult chan struct{}
	// results holds the results of the last execution, and is returned to the
	// waiting goroutines
	results *execProxyScrapeResult
	log     log.Logger
}

// execCachingProxy implements a caching proxy for metrics produced by a periodically executed script.
type execCachingProxy struct {
	commandPath  string
	arguments    []string
	execInterval time.Duration

	lastExec      time.Time
	lastResult    []*dto.MetricFamily
	resultReadyCh <-chan struct{}
	lastResultMtx *sync.RWMutex

	log log.Logger
}

// newExecProxy initializes a new execProxy and its goroutines.
func newExecProxy(config *config.ExecExporterConfig) *execProxy {
	execReqCh := make(chan struct{})

	newProxy := execProxy{
		commandPath:    config.Command,
		arguments:      config.Args,
		execRequestMtx: &sync.Mutex{},
		execRequest:    make(chan struct{}),
		execResultWg:   &sync.WaitGroup{},
		execResult:     make(chan struct{}),
		results:        nil,
		log:            log.Base(),
	}

	// Lock the request mutex initially to avoid a race
	newProxy.execRequestMtx.Lock()

	go newProxy.execer(execReqCh)

	return &newProxy
}

// doExec handles the actual application execution.
func (ep *execProxy) doExec() *execProxyScrapeResult {
	// allocate a new result struct now
	result := &execProxyScrapeResult{
		mfs: nil,
		err: nil,
	}

	ep.log.Debugln("Executing metric script to service new scrape request")
	// Have at least 1 listener, start executing.

	cmd := exec.Command(ep.commandPath, ep.arguments...) // nolint: gas
	outRdr, perr := cmd.StdoutPipe()
	if perr != nil {
		result.err = perr
		ep.log.With("error", perr.Error()).
			Errorln("Error opening stdout pipe to metric script")
		return result
	}

	if err := cmd.Start(); err != nil {
		result.err = err
		ep.log.With("error", err.Error()).
			Errorln("Error starting metric script")
		return result
	}

	mfs, derr := decodeMetrics(outRdr, expfmt.FmtText)

	// Wait for the process to exit.
	// If the process never exits, we'll wait indefinitly
	if err := cmd.Wait(); err != nil {
		result.err = err
		ep.log.With("error", err.Error()).
			Errorln("Metric script exited with error")
		return result
	}

	if derr != nil {
		result.err = derr
		ep.log.With("error", derr.Error()).
			Errorln("Metric decoding from script output failed")
		return result
	}
	result.mfs = mfs
	return result
}

func (ep *execProxy) execer(reqCh <-chan struct{}) {
	ep.log.Debugln("ExecProxy started")
	var waitCh chan struct{}
	for {
		// Make a copy of ep.execRequest so scrape can set it null safely
		waitCh = ep.execRequest

		// Unlock and allow new scrapes to make requests
		ep.execRequestMtx.Unlock()

		// Wait for a request to close the channel (and set it to nil)
		<-waitCh

		// execute the subprocess and get results
		ep.results = ep.doExec()

		// Have results, no more waiters allowed!
		ep.execRequestMtx.Lock()

		// Signal existing waiters to take results
		close(ep.execResult)

		// Wait for waiters to finish
		ep.execResultWg.Wait()

		// Replace ep.execResult channel
		ep.execResult = make(chan struct{})
		// Replace the ep.execRequest channel
		ep.execRequest = make(chan struct{})

	}
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (ep *execProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Ensure script execution is requested
	ep.execRequestMtx.Lock()
	// Get a copy of the currently active wait channel
	waitCh := ep.execResult
	ep.execResultWg.Add(1)       // Add a waiter
	defer ep.execResultWg.Done() // And defer a clean up when we get out of here - one way or another

	if ep.execRequest != nil {
		// Request script execution
		close(ep.execRequest)
		ep.execRequest = nil
	}
	ep.execRequestMtx.Unlock()

	// Wait for results or for our context to finish
	select {
	case <-waitCh:
		return ep.results.mfs, ep.results.err
	case <-ctx.Done():
		// Exiting before receiving anything
		return nil, ErrScrapeTimeoutBeforeExecFinished
	}
}

// newExecProxy initializes a new execProxy and its goroutines.
func newExecCachingProxy(config *config.ExecCachingExporterConfig) *execCachingProxy {
	rdyCh := make(chan struct{})

	newProxy := execCachingProxy{
		commandPath:  config.Command,
		arguments:    config.Args,
		execInterval: time.Duration(config.ExecInterval),

		lastResult:    make([]*dto.MetricFamily, 0),
		resultReadyCh: rdyCh,
		lastResultMtx: &sync.RWMutex{},

		log: log.Base(),
	}

	go newProxy.execer(rdyCh)

	return &newProxy
}

func (ecp *execCachingProxy) execer(rdyCh chan<- struct{}) {
	ecp.log.Debugln("ExecCachingProxy started")

	for {
		nextExec := ecp.lastExec.Add(ecp.execInterval)
		ecp.log.With("next_exec", nextExec.String()).
			Debugln("Waiting for next interval")
		<-time.After(time.Until(nextExec))
		ecp.log.Debugln("Executing metric script on timeout")

		ecp.lastExec = time.Now()
		cmd := exec.Command(ecp.commandPath, ecp.arguments...) // nolint: gas
		outRdr, perr := cmd.StdoutPipe()
		if perr != nil {
			ecp.log.With("error", perr.Error()).
				Errorln("Error opening stdout pipe to metric script")
			continue
		}

		if err := cmd.Start(); err != nil {
			ecp.log.With("error", err.Error()).
				Errorln("Error starting metric script")
			continue
		}

		//if err := cmd.Wait(); err != nil {
		//	ecp.log.With("error", err.Error()).
		//		Errorln("Metric script exited with error")
		//	continue
		//}

		mfs, derr := decodeMetrics(outRdr, expfmt.FmtText)
		// Hard kill the script once metric decoding finishes. It's the only way to be sure.
		// Maybe sigterm with a timeout?
		if err := cmd.Process.Kill(); err != nil {
			ecp.log.With("error", derr.Error()).
				Errorln("Error sending kill signal to subprocess")
		}
		if derr != nil {
			ecp.log.With("error", derr.Error()).
				Errorln("Metric decoding from script output failed")
			continue
		}

		// Cache new metrics
		ecp.lastResultMtx.Lock()
		ecp.lastResult = mfs
		if rdyCh != nil {
			// Better way?
			close(rdyCh)
			rdyCh = nil
		}
		ecp.lastResultMtx.Unlock()
	}
}

// Scrape simply retrieves the cached metrics, or waits until they are available.
func (ecp *execCachingProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	var rerr error

	select {
	case <-ecp.resultReadyCh:
		log.Debugln("Returning cached results fo scrape")
	case <-ctx.Done():
		// context cancelled before scrape finished
		rerr = ErrScrapeTimeoutBeforeExecFinished
		return []*dto.MetricFamily{}, rerr
	}

	var retMetrics []*dto.MetricFamily

	ecp.lastResultMtx.RLock()
	retMetrics = ecp.lastResult
	ecp.lastResultMtx.RUnlock()

	return retMetrics, rerr
}
