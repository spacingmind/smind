package wsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/spacingmind/smind/internal/push"
)

// Wire codes for the push.* RPCs (see rpcCodeError); the generic ones
// (codeInvalid, codeForbidden) live in learning.go.
const (
	codePushDisabled        = "push_disabled"
	codeUnsupportedProvider = "unsupported_provider"
	codeInvalidToken        = "invalid_token"
	codeInvalidKey          = "invalid_key"
	codeInvalidKinds        = "invalid_kinds"
)

// pushKinds is the canonical kind order; registering with no kinds means all.
var pushKinds = []string{"permission", "needs_continue", "done", "error"}

var expoTokenRE = regexp.MustCompile(`^Expo(nent)?PushToken\[[^\[\]\s]{1,200}\]$`)

// PushRegistry is the slice of *push.Registry the push.* methods use.
type PushRegistry interface {
	Put(reg push.Registration) error
	Get(deviceID string) (push.Registration, bool)
	Remove(deviceID string) (bool, error)
}

// PushOptions configures SetPush. Later steps add fields (a notifier); keep it a struct.
type PushOptions struct {
	Enabled  bool // false: every push.* method answers push_disabled
	Registry PushRegistry
	// Approved reports whether the device is currently approved in the relay
	// device registry; nil treats every identified device as approved.
	Approved func(deviceID string) bool
	Now      func() time.Time // default time.Now
}

// SetPush registers push.register, push.unregister and push.status. Like
// SetRelayDevices it must be called once at startup, before the API serves
// anything (it writes a.hs without locking).
func (a *API) SetPush(o PushOptions) {
	if o.Now == nil {
		o.Now = time.Now
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
		return append([]string(nil), pushKinds...), nil
	}
	want := make(map[string]bool, len(in))
	for _, k := range in {
		known := false
		for _, c := range pushKinds {
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
	for _, c := range pushKinds {
		if want[c] {
			out = append(out, c)
		}
	}
	return out, nil
}
