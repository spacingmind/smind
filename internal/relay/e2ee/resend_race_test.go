package e2ee

import (
	"errors"
	"sync"
	"testing"
)

// TestReSendFromReceiveRacesWithSend: the daemon re-sends the stored M2/R_d
// from Receive (a duplicate M1 was injected) while another goroutine Sends.
// Run with -race.
func TestReSendFromReceiveRacesWithSend(t *testing.T) {
	p := establishedPair(t)
	m1 := rawFrame(frameHello, helloPayload(ProtocolVersion, RoleMobile, p.daemon.ClientEphemeralPublicKey()))
	const n = 50

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { // the client's side: drain what the daemon sends
		defer wg.Done()
		for range n {
			if _, err := p.mobile.Receive(); err != nil {
				t.Errorf("mobile Receive: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range n {
			if err := p.daemon.Send([]byte{byte(i)}); err != nil {
				t.Errorf("daemon Send: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		// Receive handles the duplicate M1 (re-sending M2 and R_d) and then
		// stops at the undecryptable data frame behind it, leaving the channel
		// usable, while the Send goroutine above writes concurrently.
		p.daemonConn.inject(m1)
		p.daemonConn.inject(rawFrame(frameData, make([]byte, 8)))
		if _, err := p.daemon.Receive(); !errors.Is(err, ErrDecrypt) {
			t.Errorf("daemon Receive = %v, want ErrDecrypt after the re-send", err)
		}
	}()
	wg.Wait()
}
