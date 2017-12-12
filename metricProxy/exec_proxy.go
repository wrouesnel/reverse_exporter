package metricProxy

import (
	"bytes"
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
	ErrScrapeTimeoutBeforeExecFinished = errors.New("scrape timed out before exec finished")
)

// execProxy implements an efficient script metric proxy which aggregates scrapes.
type execProxy struct {
	commandPath string
	arguments   []string
	// waitingScrapes is a list of channels which are currently waiting for the results of a command executions
	waitingScrapes map[chan<- []*dto.MetricFamily]struct{}
	waitingMtx     *sync.Mutex
	// Incoming scrapes send to this channel to request results
	execReqCh chan<- struct{}
	log       log.Logger
}

// newExecProxy initializes a new execProxy and its goroutines.
func newExecProxy(config *config.ExecExporterConfig) *execProxy {
	execReqCh := make(chan struct{})

	newProxy := execProxy{
		commandPath:    config.Command,
		arguments:      config.Args,
		waitingScrapes: make(map[chan<- []*dto.MetricFamily]struct{}),
		waitingMtx:     &sync.Mutex{},
		execReqCh:      execReqCh,
		log:            log.Base(),
	}

	go newProxy.execer(execReqCh)

	return &newProxy
}

func (ep *execProxy) execer(reqCh <-chan struct{}) {
	stdoutBuffer := new(bytes.Buffer)

	ep.log.Debugln("ExecProxy started")

	for {
		<-reqCh
		// Got a request. Check there is non-zero waiting requestors (i.e. maybe this was satisifed by the
		// loop gone-by
		if len(ep.waitingScrapes) == 0 {
			// Nothing waiting, request from a previous loop.
			continue
		}
		ep.log.Debugln("Executing metric script to service new scrape request")
		// Have at least 1 listener, start executing.

		cmd := exec.Command(ep.commandPath, ep.arguments...)
		outRdr, err := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			ep.log.With("error", err.Error()).
				Errorln("Error starting metric script")
			continue
		}

		//if err := cmd.Wait(); err != nil {
		//	ep.log.With("error", err.Error()).
		//		Errorln("Metric script exited with error")
		//	continue
		//}

		mfs, err := decodeMetrics(outRdr, expfmt.FmtText)
		// Hard kill the script once metric decoding finishes. It's the only way to be sure.
		// Maybe sigterm with a timeout?
		cmd.Process.Kill()
		if err != nil {
			ep.log.With("error", err.Error()).
				Errorln("Metric decoding from script output failed")
			continue
		}

		// Emit metrics to all waiting scrapes
		ep.waitingMtx.Lock()
		ep.log.With("num_waiters", len(ep.waitingScrapes)).
			Debugln("Dispatching metrics to waiters")
		for ch := range ep.waitingScrapes {
			ch <- mfs
		}
		// Replace the scrape map since all scrapes are now satisfied.
		ep.waitingScrapes = make(map[chan<- []*dto.MetricFamily]struct{})
		ep.waitingMtx.Unlock()
		// Clear the output buffer
		stdoutBuffer.Truncate(0)
	}
}

// Scrape scrapes the underlying metric endpoint. values are URL parameters
// to be used with the request if needed.
func (ep *execProxy) Scrape(ctx context.Context, values url.Values) ([]*dto.MetricFamily, error) {
	var rerr error
	retMetrics := make([]*dto.MetricFamily, 0)

	// Lock the waiting map and add a new listener
	ep.waitingMtx.Lock()
	scrapeCh := make(chan []*dto.MetricFamily)
	ep.waitingScrapes[scrapeCh] = struct{}{}
	ep.waitingMtx.Unlock()

	// Send an execution request (important: since exec might finish before we add to the map, we must do this here)
	select {
	case ep.execReqCh <- struct{}{}:
		ep.log.Debugln("Script execution request dispatched")
	default:
		ep.log.Debugln("Script execution already pending")
	}

	// Wait for the channel to respond, or for our context to cancel
	select {
	case retMetrics = <-scrapeCh:
		// Success - return results.
		rerr = nil
	case <-ctx.Done():
		// Exiting before received anything - remove the waiting channel
		ep.waitingMtx.Lock()
		delete(ep.waitingScrapes, scrapeCh)
		ep.waitingMtx.Unlock()
		rerr = ErrScrapeTimeoutBeforeExecFinished
	}

	return retMetrics, rerr
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
	stdoutBuffer := new(bytes.Buffer)
	ecp.log.Debugln("ExecCachingProxy started")

	for {
		nextExec := ecp.lastExec.Add(ecp.execInterval)
		ecp.log.With("next_exec", nextExec.String()).
			Debugln("Waiting for next interval")
		<-time.After(nextExec.Sub(time.Now()))
		ecp.log.Debugln("Executing metric script on timeout")

		ecp.lastExec = time.Now()
		cmd := exec.Command(ecp.commandPath, ecp.arguments...)
		outRdr, err := cmd.StdoutPipe()
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

		mfs, err := decodeMetrics(outRdr, expfmt.FmtText)
		// Hard kill the script once metric decoding finishes. It's the only way to be sure.
		// Maybe sigterm with a timeout?
		cmd.Process.Kill()
		if err != nil {
			ecp.log.With("error", err.Error()).
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

		// Clear the output buffer
		stdoutBuffer.Truncate(0)
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
