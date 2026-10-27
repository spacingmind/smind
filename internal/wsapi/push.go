package wsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/push"
	"github.com/spacingmind/smind/internal/runs"
)

// Wire codes for the push.* RPCs (see rpcCodeError); the generic ones
// (codeInvalid, codeForbidden) live in learning.go.
const (
	codePushDisabled        = "push_disabled"
	codeUnsupportedProvider = "unsupported_provider"
	codeInvalidToken        = "invalid_token"
	codeInvalidKey          = "invalid_key"
	codeInvalidKinds        = "invalid_kinds"
	codeAmbiguous           = "ambiguous"
)

var expoTokenRE = regexp.MustCompile(`^Expo(nent)?PushToken\[[^\[\]\s]{1,200}\]$`)

// PushRegistry is the slice of *push.Registry the push.* methods use.
type PushRegistry interface {
	Put(reg push.Registration) error
	Get(deviceID string) (push.Registration, bool)
	Remove(deviceID string) (bool, error)
	List() []push.Registration
}

// PushOptions configures SetPush. Later steps add fields (a notifier); keep it a struct.
type PushOptions struct {
	Enabled  bool // false: every push.* method answers push_disabled
	Registry PushRegistry
	// Approved reports whether the device is currently approved in the relay
	// device registry; nil treats every identified device as approved.
	Approved func(deviceID string) bool
	Now      func() time.Time // default time.Now
	// Notifier turns bus events into pushes; nil means nothing is sent.
	// SetPush starts it and taps the event bus when Enabled.
	Notifier *push.Notifier
}

// SetPush registers push.register, push.unregister, push.status and push.test. Like
// SetRelayDevices it must be called once at startup, before the API serves
// anything (it writes a.hs without locking).
func (a *API) SetPush(o PushOptions) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Enabled && o.Notifier != nil {
		o.Notifier.Start()
		a.bus.setTap(func(e Event) {
			if in, ok := pushInput(e); ok {
				o.Notifier.Enqueue(in)
			}
		})
	}
	if o.Enabled && o.Registry != nil {
		a.pushRegistered = func(id string) bool { _, ok := o.Registry.Get(id); return ok }
	}
	a.hs["push.register"] = pushGuard(o, func(rc *requestContext, raw json.RawMessage) (any, error) {
		return pushRegister(o, rc, raw)
	})
	a.hs["push.unregister"] = pushGuard(o, func(rc *requestContext, _ json.RawMessage) (any, error) {
		if _, err := o.Registry.Remove(rc.deviceID); err != nil {
			return nil, fmt.Errorf("push.unregister: %w", err)
		}
		return pushUnregisterResult{V: 1}, nil
	})
	a.hs["push.status"] = pushGuard(o, func(rc *requestContext, _ json.RawMessage) (any, error) {
		res := pushStatusResult{V: 1, Enabled: true}
		if reg, ok := o.Registry.Get(rc.deviceID); ok {
			res.Registered = true
			res.Provider = reg.Provider
			res.Platform = reg.Platform
			res.Kinds = reg.Kinds
		}
		return res, nil
	})
	a.hs["push.test"] = func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		return pushTest(ctx, o, rc, raw)
	}
}

const minPushPrefix = 4

type pushTestParams struct {
	DeviceID string `json:"deviceId"`
}

type pushTestResult struct {
	Results []push.TestResult `json:"results"`
}

// pushTest sends a test push to one (by id prefix) or every registered device.
// Local connection only; it does not use pushGuard because the local /ws
// connection has no device id.
func pushTest(ctx context.Context, o PushOptions, rc *requestContext, raw json.RawMessage) (any, error) {
	if !o.Enabled || o.Notifier == nil {
		return nil, &rpcCodeError{code: codePushDisabled, msg: "push notifications are disabled on this daemon"}
	}
	if !rc.local {
		return nil, &rpcCodeError{code: codeForbidden, msg: "push.test is only available on the local connection"}
	}
	var p pushTestParams
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &rpcCodeError{code: codeInvalid, msg: "push.test: invalid params"}
		}
	}
	prefix := strings.ToLower(p.DeviceID)
	if prefix != "" && len(prefix) < minPushPrefix {
		return nil, &rpcCodeError{code: codeInvalid, msg: "push.test: deviceId prefix must be at least 4 characters"}
	}
	regs := o.Registry.List()
	ids := make([]string, 0, len(regs))
	var matches []string
	for _, r := range regs {
		ids = append(ids, r.DeviceID)
		if prefix == "" {
			continue
		}
		lid := strings.ToLower(r.DeviceID)
		if lid == prefix {
			matches = []string{r.DeviceID}
			break
		}
		if strings.HasPrefix(lid, prefix) {
			matches = append(matches, r.DeviceID)
		}
	}
	if prefix != "" {
		switch len(matches) {
		case 0:
			return nil, &rpcCodeError{code: codeNotFound, msg: "no push registration for that device"}
		case 1:
			ids = matches
		default:
			return nil, &rpcCodeError{code: codeAmbiguous, msg: "deviceId prefix matches more than one push registration"}
		}
	}
	if len(ids) == 0 {
		return pushTestResult{Results: []push.TestResult{}}, nil
	}
	return pushTestResult{Results: o.Notifier.Test(ctx, ids)}, nil
}

type pushRegisterParams struct {
	V        int      `json:"v"`
	Provider string   `json:"provider"`
	Token    string   `json:"token"`
	Platform string   `json:"platform"`
	Key      string   `json:"key"`
	Kinds    []string `json:"kinds"`
}

type pushRegisterResult struct {
	V          int      `json:"v"`
	Registered bool     `json:"registered"`
	Kinds      []string `json:"kinds"`
}

type pushUnregisterResult struct {
	V          int  `json:"v"`
	Registered bool `json:"registered"`
}

type pushStatusResult struct {
	V          int      `json:"v"`
	Enabled    bool     `json:"enabled"`
	Registered bool     `json:"registered"`
	Provider   string   `json:"provider,omitempty"`
	Platform   string   `json:"platform,omitempty"`
	Kinds      []string `json:"kinds,omitempty"`
}

// pushGuard applies the common checks: enabled, identified device, approved.
func pushGuard(o PushOptions, h func(rc *requestContext, raw json.RawMessage) (any, error)) handlerFunc {
	return func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if !o.Enabled {
			return nil, &rpcCodeError{code: codePushDisabled, msg: "push notifications are disabled on this daemon"}
		}
		if rc.deviceID == "" {
			return nil, &rpcCodeError{code: codeForbidden, msg: "push.* needs a paired relay device"}
		}
		if o.Approved != nil && !o.Approved(rc.deviceID) {
			return nil, &rpcCodeError{code: codeForbidden, msg: "device is not approved"}
		}
		return h(rc, raw)
	}
}

func pushRegister(o PushOptions, rc *requestContext, raw json.RawMessage) (any, error) {
	var p pushRegisterParams
	if err := json.Unmarshal(raw, &p); err != nil || p.V != 1 {
		return nil, &rpcCodeError{code: codeInvalid, msg: "push.register: invalid params"}
	}
	if p.Provider != push.ProviderExpo {
		return nil, &rpcCodeError{code: codeUnsupportedProvider, msg: "push.register: unsupported provider"}
	}
	if p.Platform != push.PlatformAndroid && p.Platform != push.PlatformIOS {
		return nil, &rpcCodeError{code: codeInvalid, msg: "push.register: platform must be android or ios"}
	}
	if !expoTokenRE.MatchString(p.Token) {
		return nil, &rpcCodeError{code: codeInvalidToken, msg: "push.register: invalid push token"}
	}
	key, err := base64.StdEncoding.DecodeString(p.Key)
	if err != nil || len(key) != push.KeySize {
		return nil, &rpcCodeError{code: codeInvalidKey, msg: "push.register: key must be base64 of 32 bytes"}
	}
	kinds, err := normalizePushKinds(p.Kinds)
	if err != nil {
		return nil, err
	}
	reg := push.Registration{
		DeviceID:     rc.deviceID,
		Provider:     p.Provider,
		Token:        p.Token,
		Platform:     p.Platform,
		Key:          key,
		Kinds:        kinds,
		RegisteredAt: o.Now().UTC(),
	}
	if err := o.Registry.Put(reg); err != nil {
		return nil, fmt.Errorf("push.register: %w", err)
	}
	return pushRegisterResult{V: 1, Registered: true, Kinds: kinds}, nil
}

// normalizePushKinds validates kinds, collapses duplicates and returns them in
// canonical order; empty means all.
func normalizePushKinds(in []string) ([]string, error) {
	if len(in) == 0 {
		return append([]string(nil), push.AllKinds...), nil
	}
	want := make(map[string]bool, len(in))
	for _, k := range in {
		known := false
		for _, c := range push.AllKinds {
			if k == c {
				known = true
				break
			}
		}
		if !known {
			return nil, &rpcCodeError{code: codeInvalidKinds, msg: "push.register: invalid kinds"}
		}
		want[k] = true
	}
	out := make([]string, 0, len(want))
	for _, c := range push.AllKinds {
		if want[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

// pushInput maps a bus event to a notifier input; ok is false for events that
// never notify.
func pushInput(e Event) (push.Input, bool) {
	switch e.Topic {
	case TopicPermissionPending:
		p, ok := e.Payload.(permissionPendingPayload)
		if !ok {
			return push.Input{}, false
		}
		return push.Input{Kind: push.KindPermission, Key: p.RequestID, TaskID: p.TaskID, ChatID: p.ChatID, RunID: p.RunID, Summary: p.Summary}, true
	case TopicAutomationPaused:
		// ADR-0044 question 7: an auto-pause always notifies. It rides the
		// existing "error" kind (devices already subscribe to it, mobile
		// already renders it) and opens the failed task; a pause with no task
		// has nothing to open and is not pushed.
		p, ok := e.Payload.(automationPausedPayload)
		if !ok || p.TaskID <= 0 {
			return push.Input{}, false
		}
		return push.Input{Kind: push.KindError, Key: "autopause:" + strconv.FormatInt(p.AutomationID, 10) + ":" + strconv.FormatInt(p.TaskID, 10), TaskID: p.TaskID, Summary: "Automation paused: " + p.Reason}, true
	case TopicTaskStalled:
		p, ok := e.Payload.(stalledPayload)
		if !ok {
			return push.Input{}, false
		}
		since, err := time.Parse(time.RFC3339, p.Since)
		if err != nil {
			return push.Input{}, false
		}
		return push.Input{Kind: push.KindStalled, Key: "stall:" + strconv.FormatInt(p.TaskID, 10) + ":" + strconv.FormatInt(since.Unix(), 10), TaskID: p.TaskID, Minutes: p.Minutes}, true
	case TopicRunStatus:
		p, ok := e.Payload.(runStatusPayload)
		if !ok {
			return push.Input{}, false
		}
		in := push.Input{TaskID: p.TaskID, ChatID: p.ChatID, RunID: p.RunID}
		switch p.Status {
		case "done":
			in.Kind = push.KindDone
			if p.Disposition == runs.DispositionNeedsContinue {
				in.Kind = push.KindNeedsContinue
			}
		case "error":
			in.Kind = push.KindError
			in.Err = p.Err
		default:
			return push.Input{}, false
		}
		in.Key = p.RunID + ":" + in.Kind
		return in, true
	}
	return push.Input{}, false
}
