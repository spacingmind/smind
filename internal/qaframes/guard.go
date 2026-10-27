package qaframes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HaikuModel is the only model a scripted send may use (standing rule: a
// screenshot script's live calls are pinned to the cheap model).
const HaikuModel = "claude-haiku-5-5"

// ForbiddenPorts are the ports of the Human's own daemons.
var ForbiddenPorts = []int{4648, 4870}

// Step is one scenario step; the driver defines the vocabulary.
type Step struct {
	Do        string `json:"do"`
	Route     string `json:"route,omitempty"`
	Sel       string `json:"sel,omitempty"`
	SendSel   string `json:"sendSel,omitempty"`
	Text      string `json:"text,omitempty"`
	Key       string `json:"key,omitempty"`
	Name      string `json:"name,omitempty"`
	Ms        int    `json:"ms,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	Frames    int    `json:"frames,omitempty"`
	GapMs     int    `json:"gapMs,omitempty"`
	Exact     bool   `json:"exact,omitempty"`
	Then      *Step  `json:"then,omitempty"`
}

// ErrSendRefused is returned before any process starts when a scenario sends a
// prompt without explicit permission and a pinned haiku model.
var ErrSendRefused = errors.New("scripted send refused")

func hasSend(s *Step) bool {
	for ; s != nil; s = s.Then {
		if s.Do == "send" {
			return true
		}
	}
	return false
}

// CheckSends enforces the send guard over every scenario: a step that sends a
// prompt needs allowSend (off by default) and model == HaikuModel exactly.
func CheckSends(scenarios map[string][]Step, allowSend bool, model string) error {
	for id, steps := range scenarios {
		for i := range steps {
			if !hasSend(&steps[i]) {
				continue
			}
			if !allowSend {
				return fmt.Errorf("%w: frame %s sends a prompt but --allow-send is not set", ErrSendRefused, id)
			}
			if model != HaikuModel {
				return fmt.Errorf("%w: frame %s sends a prompt and --model is %q, only %q is allowed", ErrSendRefused, id, model, HaikuModel)
			}
		}
	}
	return nil
}

// CheckIsolation refuses a SMIND_HOME or port that could touch the Human's own
// daemon: the default ~/.smind, the caller's own SMIND_HOME, the caller's home
// directory, or a forbidden port. home must be an existing directory.
func CheckIsolation(home string, port int, callerHome, callerSmindHome string) error {
	for _, p := range ForbiddenPorts {
		if port == p {
			return fmt.Errorf("port %d is a real daemon's port", port)
		}
	}
	if port <= 0 {
		return fmt.Errorf("invalid port %d", port)
	}
	h, err := filepath.EvalSymlinks(home)
	if err != nil {
		return err
	}
	for _, b := range []string{callerSmindHome, filepath.Join(callerHome, ".smind"), callerHome} {
		if b == "" || b == ".smind" {
			continue
		}
		if r, err := filepath.EvalSymlinks(b); err == nil {
			b = r
		}
		if h == b {
			return fmt.Errorf("home %s is the real smind home", home)
		}
	}
	if st, err := os.Stat(h); err != nil || !st.IsDir() {
		return fmt.Errorf("home %s is not a directory", home)
	}
	return nil
}
