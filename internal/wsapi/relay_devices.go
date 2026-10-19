package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/relay/devices"
)

// RelayDeviceAdmin is what the relay.device.* methods administer: the daemon
// bridge's device hub (internal/relay/bridge.Hub). Ids may be passed as unique
// prefixes; the handlers resolve them with ResolveDevice first.
type RelayDeviceAdmin interface {
	Devices() []devices.Info
	ResolveDevice(prefix string) (string, error)
	ApproveDevice(id string) (devices.Device, error)
	RevokeDevice(id string) (devices.Device, error)
	RenameDevice(id, label string) (devices.Device, error)
	OpenPairingWindow(d time.Duration, maxNew int) error
	PairingWindow() (devices.Window, bool)
}

// SetRelayDevices registers the relay.device.* methods (ADR-0031 D9). It must
// be called at most once, at startup before the API serves any connection (it
// writes the shared handler table without locking).
//
// The methods work only on the token-authenticated local /ws connection; a
// relay-served session (ServeTransport) gets a "forbidden" error, so a paired
// device can never approve or revoke devices.
func (a *API) SetRelayDevices(admin RelayDeviceAdmin) {
	a.hs["relay.device.list"] = localOnly("relay.device.list", handleRelayDeviceList(admin))
	a.hs["relay.device.approve"] = localOnly("relay.device.approve", handleRelayDeviceByID(admin, "approve", func(id string, _ relayDeviceParams) (devices.Device, error) { return admin.ApproveDevice(id) }))
	a.hs["relay.device.revoke"] = localOnly("relay.device.revoke", handleRelayDeviceByID(admin, "revoke", func(id string, _ relayDeviceParams) (devices.Device, error) { return admin.RevokeDevice(id) }))
	a.hs["relay.device.rename"] = localOnly("relay.device.rename", handleRelayDeviceByID(admin, "rename", func(id string, p relayDeviceParams) (devices.Device, error) { return admin.RenameDevice(id, p.Label) }))
	a.hs["relay.device.window"] = localOnly("relay.device.window", handleRelayDeviceWindow(admin))
}

// rpcCodeError is an error carrying a machine-readable wire code.
type rpcCodeError struct {
	code string
	msg  string
	err  error
	data any
}

func (e *rpcCodeError) RPCData() any { return e.data }

func (e *rpcCodeError) Error() string   { return e.msg }
func (e *rpcCodeError) RPCCode() string { return e.code }
func (e *rpcCodeError) Unwrap() error   { return e.err }

// localOnly refuses h on any connection that is not the local /ws one.
func localOnly(method string, h handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if !rc.local {
			return nil, &rpcCodeError{code: "forbidden", msg: "relay.device.* is not available over the relay"}
		}
		return h(ctx, rc, raw)
	}
}

type relayDeviceParams struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type relayDeviceWindow struct {
	ExpiresAt time.Time `json:"expires_at"`
	Remaining int       `json:"remaining"`
}

func windowResult(w devices.Window, ok bool) *relayDeviceWindow {
	if !ok {
		return nil
	}
	return &relayDeviceWindow{ExpiresAt: w.ExpiresAt, Remaining: w.Remaining}
}

type relayDeviceListResult struct {
	Devices []devices.Info     `json:"devices"`
	Window  *relayDeviceWindow `json:"window"`
}

// relayDeviceError maps the devices package's sentinel errors to wire codes.
func relayDeviceError(method string, err error) error {
	switch {
	case errors.Is(err, devices.ErrNotFound):
		return &rpcCodeError{code: "not_found", msg: method + ": no such device", err: err}
	case errors.Is(err, devices.ErrAmbiguous):
		return &rpcCodeError{code: "ambiguous", msg: method + ": device id prefix is ambiguous; use more characters", err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

func handleRelayDeviceList(admin RelayDeviceAdmin) handlerFunc {
	return func(context.Context, *requestContext, json.RawMessage) (any, error) {
		list := admin.Devices()
		if list == nil {
			list = []devices.Info{}
		}
		w, ok := admin.PairingWindow()
		return relayDeviceListResult{Devices: list, Window: windowResult(w, ok)}, nil
	}
}

func handleRelayDeviceByID(admin RelayDeviceAdmin, verb string, act func(id string, p relayDeviceParams) (devices.Device, error)) handlerFunc {
	method := "relay.device." + verb
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p relayDeviceParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("%s: invalid params: %w", method, err)
		}
		if p.ID == "" {
			return nil, fmt.Errorf("%s: id is required", method)
		}
		id, err := admin.ResolveDevice(p.ID)
		if err != nil {
			return nil, relayDeviceError(method, err)
		}
		d, err := act(id, p)
		if err != nil {
			return nil, relayDeviceError(method, err)
		}
		return d, nil
	}
}

func handleRelayDeviceWindow(admin RelayDeviceAdmin) handlerFunc {
	return func(_ context.Context, _ *requestContext, raw json.RawMessage) (any, error) {
		var p struct {
			DurationSeconds int `json:"duration_seconds"`
			MaxDevices      int `json:"max_devices"`
		}
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("relay.device.window: invalid params: %w", err)
			}
		}
		if p.DurationSeconds < 0 || p.MaxDevices < 0 {
			return nil, errors.New("relay.device.window: duration_seconds and max_devices must not be negative")
		}
		// 0 means "registry default" (10 minutes, 1 device).
		if err := admin.OpenPairingWindow(time.Duration(p.DurationSeconds)*time.Second, p.MaxDevices); err != nil {
			return nil, fmt.Errorf("relay.device.window: %w", err)
		}
		w, ok := admin.PairingWindow()
		return windowResult(w, ok), nil
	}
}
