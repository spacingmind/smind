package main

import (
	"database/sql"
	"errors"
	"log"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/push"
	"github.com/spacingmind/smind/internal/relay/bridge"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/wsapi"
)

// pushAPI is the slice of *wsapi.API wirePush needs.
type pushAPI interface {
	SetPush(wsapi.PushOptions)
}

// pushRuntime holds the pieces of mobile push (ADR-0040) that outlive wiring.
// The zero value (push off) is valid.
type pushRuntime struct {
	notifier *push.Notifier
	registry *push.Registry
}

// Close stops the notifier worker (aborting an in-flight send). Nil-safe and
// idempotent.
func (p *pushRuntime) Close() {
	if p == nil || p.notifier == nil {
		return
	}
	p.notifier.Close()
}

// taskInfoFor looks tasks up for the notifier's rules.
func taskInfoFor(db *store.Store) func(int64) (push.TaskInfo, bool) {
	return func(id int64) (push.TaskInfo, bool) {
		t, err := db.GetTask(id)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				log.Printf("push: task lookup failed")
			}
			return push.TaskInfo{}, false
		}
		return push.TaskInfo{
			Title:  t.Title,
			Root:   t.ParentTaskID == nil,
			System: t.Kind == store.TaskKindSystem,
		}, true
	}
}

// wirePush registers the push.* methods and, when enabled, starts the notifier
// and taps the event bus. Like SetRelayDevices it must run before the API
// serves any connection. It never logs a push token or the Expo access token.
func wirePush(api pushAPI, hub *bridge.Hub, db *store.Store, cfg config.PushConfig) *pushRuntime {
	if !cfg.Enabled {
		api.SetPush(wsapi.PushOptions{Enabled: false})
		log.Printf("push: disabled by configuration")
		return &pushRuntime{}
	}
	reg, err := push.OpenRegistry(push.RegistryPath(config.Dir()))
	if err != nil {
		// Never reset or overwrite an unreadable registry: it holds the only
		// copy of each device's push key.
		log.Printf("push: %v (push notifications disabled until the file is fixed)", err)
		api.SetPush(wsapi.PushOptions{Enabled: false})
		return &pushRuntime{}
	}
	sender := push.NewExpo(push.ExpoOptions{AccessToken: cfg.ExpoAccessToken, Logf: log.Printf})
	notifier := push.NewNotifier(push.Deps{
		Registry: reg,
		Sender:   sender,
		Approved: hub.DeviceApproved,
		Task:     taskInfoFor(db),
		Logf:     log.Printf,
	})
	api.SetPush(wsapi.PushOptions{Enabled: true, Registry: reg, Approved: hub.DeviceApproved, Notifier: notifier})
	log.Printf("push: enabled (%d registered devices)", len(reg.List()))
	return &pushRuntime{notifier: notifier, registry: reg}
}
