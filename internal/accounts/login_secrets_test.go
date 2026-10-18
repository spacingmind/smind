package accounts

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// TestLoginSecretsNeverAppearInLogsOrErrors is "login secrets never appear in
// logs or errors": whatever a login is fed -- a good paste, a wrong-state
// paste, a malformed paste, an unknown loginId, a mismatched listener
// callback -- the code, state and pasted URL stay out of every error and out
// of everything logged. Not parallel: it swaps the process-wide log and slog
// destinations.
func TestLoginSecretsNeverAppearInLogsOrErrors(t *testing.T) {
	var logs bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	prevSlog := slog.Default()
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		slog.SetDefault(prevSlog)
	})

	const (
		secretCode  = "SECRETCODE-9f8e7d"
		secretWrong = "WRONGSTATE-1a2b3c"
		secretJunk  = "JUNKPASTE-4d5e6f"
	)

	coord, _, login := newCompleteCoordinator(t)
	var errs []string
	record := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	// First login: rejected pastes, then a good one.
	s := startFakeLogin(t, coord, login, "secrets")
	pastedWrong := "http://localhost:54545/callback?code=" + secretCode + "&state=" + secretWrong
	pastedJunk := "https://example.com/" + secretJunk
	pastedRight := "http://localhost:54545/callback?code=" + secretCode + "&state=" + s.state
	record(coord.Complete(s.loginID, pastedWrong))
	record(coord.Complete(s.loginID, pastedJunk))
	record(coord.Complete(s.loginID, secretCode+"#"+secretWrong))
	record(coord.Complete("unknown-id", pastedRight))
	if len(errs) != 4 {
		t.Fatalf("collected %d errors %q, want 4 rejected pastes", len(errs), errs)
	}
	if err := coord.Complete(s.loginID, pastedRight); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if o := s.wait(t); o.err != nil {
		t.Fatalf("Login() error = %v", o.err)
	}

	// Second login: a mismatched listener callback and an exhausted attempt
	// budget.
	s2 := startFakeLogin(t, coord, login, "secrets2")
	getCallback(t, login.addr, secretCode, secretWrong)
	o := s2.wait(t)
	record(o.err)
	s3 := startFakeLogin(t, coord, login, "secrets3")
	for i := 0; i < maxCompleteAttempts+1; i++ {
		record(coord.Complete(s3.loginID, pastedWrong))
	}

	secrets := []string{secretCode, secretWrong, secretJunk, s.state, s2.state, pastedWrong, pastedJunk, pastedRight}
	for _, secret := range secrets {
		for _, msg := range errs {
			if strings.Contains(msg, secret) {
				t.Errorf("an error leaks %q: %q", secret, msg)
			}
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs leak %q: %q", secret, logs.String())
		}
	}
}
