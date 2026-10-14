package relaytest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/relay/client"
	"github.com/spacingmind/smind/internal/relay/e2ee"
	relaypb "github.com/spacingmind/smind/internal/relay/relaypb"
)

// endpoint is one end of an E2EE session over the relay plus a receive pump:
// shared by Device and the Daemon stub's per-device routes.
//
// Why a pump and a tap instead of DataConn.DropTransport/Close: both take the
// read lock that a blocked Receive holds, so they cannot be called while a
// receive loop runs. The endpoint instead owns the stream context (cancel
// unblocks the pump) and half-closes the stream through the tap, then waits
// for the pump to see the end -- which the relay only sends after its handler
// returned, so the drop is known to be processed.
type endpoint struct {
	conn  *client.DataConn
	tap   *tapClient
	admID string

	ctx    context.Context
	cancel context.CancelFunc

	onMsg func([]byte)

	mu  sync.Mutex
	run *pumpRun
}

// pumpRun is one receive-loop incarnation (a new one per resume).
type pumpRun struct {
	done chan struct{}
	err  error // set before done closes
}

type endpointConfig struct {
	rc                   relaypb.RelayClient
	admID, host, session string
	deviceID             string
	kp                   *e2ee.KeyPair
	role                 e2ee.Role
	pin                  []byte // expected peer key, nil = none
	onMsg                func([]byte)
}

// openEndpoint opens the data stream and completes a fresh E2EE handshake.
// The pump is not started; call startPump.
func openEndpoint(cfg endpointConfig) (*endpoint, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tap := &tapClient{RelayClient: cfg.rc}
	var opts []client.DataOption
	if cfg.pin != nil {
		opts = append(opts, client.WithExpectedPeerKey(cfg.pin))
	}
	conn, err := client.OpenData(ctx, tap, cfg.admID, cfg.host, cfg.session, cfg.deviceID, cfg.kp, cfg.role, opts...)
	if err != nil {
		cancel()
		return nil, err
	}
	e := &endpoint{conn: conn, tap: tap, admID: cfg.admID, ctx: ctx, cancel: cancel, onMsg: cfg.onMsg}
	hsCtx, hsCancel := context.WithTimeout(ctx, Timeout)
	defer hsCancel()
	if err := conn.Handshake(hsCtx); err != nil {
		e.close()
		return nil, err
	}
	return e, nil
}

// startPump runs the receive loop, calling onMsg per plaintext message, until
// the stream ends; onEnd (may be nil) is called with the run and the error
// just before the run is marked done.
func (e *endpoint) startPump(onEnd func(*pumpRun)) *pumpRun {
	run := &pumpRun{done: make(chan struct{})}
	e.mu.Lock()
	e.run = run
	e.mu.Unlock()
	go func() {
		defer close(run.done)
		for {
			msg, err := e.conn.Receive()
			if err != nil {
				run.err = err
				if onEnd != nil {
					onEnd(run)
				}
				return
			}
			e.onMsg(msg)
		}
	}()
	return run
}

func (e *endpoint) currentRun() *pumpRun {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.run
}

// dropTransport models a network drop that keeps the E2EE session: the stream
// is half-closed and the call returns once the pump saw it end, i.e. once the
// relay has processed the drop.
func (e *endpoint) dropTransport() error {
	run := e.currentRun()
	if run == nil {
		return fmt.Errorf("relaytest: no running transport to drop")
	}
	_ = e.tap.closeSend()
	select {
	case <-run.done:
		return nil
	case <-time.After(Timeout):
		return fmt.Errorf("relaytest: transport did not end after half-close")
	}
}

// resume reopens the transport keeping the session (DataConn.Resume), so the
// relay's buffered frames decrypt, and restarts the pump.
func (e *endpoint) resume(onEnd func(*pumpRun)) error {
	if err := e.conn.Resume(e.ctx, e.tap, e.admID); err != nil {
		return err
	}
	e.startPump(onEnd)
	return nil
}

func (e *endpoint) send(msg []byte) error { return e.conn.Send(msg) }

// close cancels the stream, waits for the pump and tears the session down.
func (e *endpoint) close() {
	e.cancel()
	if run := e.currentRun(); run != nil {
		<-run.done
	}
	_ = e.conn.Close()
}
