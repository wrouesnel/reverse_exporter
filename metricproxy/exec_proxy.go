package metricproxy

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
	// waitingScrapes is a map of channels which indicates the number of waiting scrape requests
	waitingScrapes map[<-chan *execProxyScrapeResult]chan<- *execProxyScrapeResult
	// drainMtx prevents new scrapes being accepted while results are being distributed.
	drainMtx *sync.Mutex
	// shouldScrapeCond is signalled whenever scrapes enter or leave
	scrapeEventCond *sync.Cond
	log             log.Logger
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
	newProxy := execProxy{
		commandPath:     config.Command,
		arguments:       config.Args,
		waitingScrapes:  map[<-chan *execProxyScrapeResult]chan<- *execProxyScrapeResult{},
		drainMtx:        &sync.Mutex{},
		scrapeEventCond: sync.NewCond(&sync.Mutex{}),
		log:             log.Base(),
	}

	go newProxy.execer()

	return &newProxy
}

// doExec handles the actual application execution. ctx, when cancelled, cancel's all execution
func (ep *execProxy) doExec(ctx context.Context) *execProxyScrapeResult {
	// allocate a new result struct now
	result := &execProxyScrapeResult{
		mfs: nil,
		err: nil,
	}

	ep.log.Debugln("Executing metric script")
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

	finished := make(chan struct{})

	// Start a watcher on the number of requestors. If it drops to 0, then kill the process
	// and terminate.
	go func() {
		select {
		case <-ctx.Done():
			ep.log.Infoln("Context done (no more scapers) - killing subprocess.")
			err := cmd.Process.Kill()
			if err != nil {
				ep.log.Errorln("Error during subprocess kill:", err.Error())
			}
		case <-finished:
			// Cancel the context listen
			return
		}
	}()

	mfs, derr := decodeMetrics(outRdr, expfmt.FmtText)

	// Wait for the process to exit.
	werr := cmd.Wait()
	ep.log.Debugln("Subprocess finished.")
	close(finished) // Disable the watchdog above
	if werr != nil {
		result.err = werr
		ep.log.With("error", werr.Error()).
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

func (ep *execProxy) execer() {
	ep.log.Debugln("ExecProxy started")

	for {
		ep.scrapeEventCond.L.Lock()
		// Wait for some scrapes to arrive
		for len(ep.waitingScrapes) == 0 {
			ep.scrapeEventCond.Wait()
		}

		// Have waiting scrapes, kick off the the execer
		ctx, cancelFn := context.WithCancel(context.Background())

		// Wait for more scrape events and cancel the exec if we drop back to 0 before finishing
		// (note we are implicitly using the lock from the outer loop)
		done := new(bool)
		*done = false
		finishedCh := make(chan struct{})
		go func(done *bool, doneCh chan<- struct{}) {
			ep.scrapeEventCond.L.Lock()
			// Watch for number of waiting scrapes to fall to 0
			for len(ep.waitingScrapes) != 0 || *done {
				ep.scrapeEventCond.Wait()
			}
			cancelFn()
			if *done {
				ep.log.Debugln("Watcher exiting after successful execution.")
			} else {
				ep.log.Debugln("No more listeners, watcher requested subprocess exit")
			}
			ep.scrapeEventCond.L.Unlock()
			close(doneCh)
		}(done, finishedCh)

		// Allow the above goroutine to start
		ep.scrapeEventCond.L.Unlock()

		// doExec always returns results (since the goroutine above will cause it's subprocess to
		// force kill if everyone gives up on it.
		results := ep.doExec(ctx)

		// Dispatch results
		ep.scrapeEventCond.L.Lock()

		ep.log.Debugln("Emitting results to remaining scrapers")
		for _, outCh := range ep.waitingScrapes {
			outCh <- results
		}
		// Ensure the watcher routine above exits
		*done = true

		// Order is important here - lock drainMtx to block new scrapers
		ep.log.Debugln("Waiting for scrapers to finish")
		ep.drainMtx.Lock()
		ep.scrapeEventCond.L.Unlock()

		// We're unlocked now, wait for the watcher routine above to close the finishedCh when
		// number of waiting scrapes falls to 0
		<-finishedCh
		ep.drainMtx.Unlock() // Allow new scrapers to start accumulating
	}
}

// newScrapeRequest adds a channel to the list of waiting channels
func (ep *execProxy) newScrapeRequest() <-chan *execProxyScrapeResult {
	// This forms part of a double mutex setup which allows old requests to drain.
	// See execer for implementations (basically drainMtx is locked while old requests
	// are cleaning up after results have been distributed).
	ep.drainMtx.Lock()

	ep.scrapeEventCond.L.Lock()

	// Add scrape (use buffered channel to avoid blocking when scrapers would like to exit)
	waitCh := make(chan *execProxyScrapeResult, 1)
	ep.waitingScrapes[waitCh] = waitCh

	ep.scrapeEventCond.L.Unlock()

	// Signal new scrape event
	ep.scrapeEventCond.Broadcast()

	ep.drainMtx.Unlock()

	return waitCh
}

// delScrapeRequest removes a request from the list of waiting channels
func (ep *execProxy) delScrapeRequest(waitCh <-chan *execProxyScrapeResult) {

	ep.scrapeEventCond.L.Lock()

	// Delete waiting scrape
	delete(ep.waitingScrapes, waitCh)

	ep.scrapeEventCond.L.Unlock()

	// Signal new scrape event
	ep.scrapeEventCond.Broadcast()
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (ep *execProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	// Get a new waitCh
	waitCh := ep.newScrapeRequest()

	defer ep.delScrapeRequest(waitCh) // Always clean up request afterwards

	// Wait for results or for our context to finish
	select {
	case results := <-waitCh:
		ep.log.Debugln("Scraper exiting with results")
		return results.mfs, results.err
	case <-ctx.Done():
		ep.log.Debugln("Scraper exiting due to context finished")
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
