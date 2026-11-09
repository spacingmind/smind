package profiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// This file is the role pack's sync (ADR-0058): comparing the profiles of an existing
// install with the current pack, per seat and per field, and applying the updates the
// Human did not make impossible. Nothing here runs by itself.

// PackVersion labels the pack (bumped with any seat text, together with rolepack_history.go):
// a name for humans and for backups; the hashes decide what is stale.
const PackVersion = 9

// Field kinds: prompt fields are what the daemon may update for an untouched profile;
// capability fields change what a seat may do and are never changed without an explicit take.
const (
	KindPrompt     = "prompt"
	KindCapability = "capability"
)

// Field states.
const (
	StateCurrent    = "current"    // equals the current pack text
	StateStale      = "stale"      // equals what an earlier release seeded
	StateCustomised = "customised" // equals neither: the Human's
)

// Seat statuses of a sync check.
const (
	SeatOK      = "ok"
	SeatMissing = "missing" // no profile of the seat's name (seeding creates it)
	SeatOwned   = "owned"   // a plugin-owned profile: read-only, never written
)

// syncedFields lists the fields the pack owns, in report order.
var syncedFields = []struct{ name, kind string }{
	{"instructions", KindPrompt},
	{"notes", KindPrompt},
	{"skills", KindPrompt},
	{"model", KindCapability},
	{"permissionMode", KindCapability},
	{"orchestrationTools", KindCapability},
	{"childPolicy", KindCapability},
	{"onRateLimit", KindCapability},
}

// fieldValue is p's value of a synced field as text.
func fieldValue(p store.AgentProfile, name string) string {
	switch name {
	case "instructions":
		return p.Instructions
	case "notes":
		return p.Notes
	case "skills":
		return strings.Join(p.Skills, "\n")
	case "model":
		return p.Model
	case "permissionMode":
		return p.PermissionMode
	case "orchestrationTools":
		if p.OrchestrationTools == nil {
			return "unset"
		}
		return fmt.Sprint(*p.OrchestrationTools)
	case "childPolicy":
		return p.ChildPolicy
	case "onRateLimit":
		if p.OnRateLimit == "" {
			return "none" // how the store reads a profile that never set it
		}
		return p.OnRateLimit
	}
	return ""
}

// setField copies the synced field name of src into dst.
func setField(dst *store.AgentProfile, src store.AgentProfile, name string) {
	switch name {
	case "instructions":
		dst.Instructions = src.Instructions
	case "notes":
		dst.Notes = src.Notes
	case "skills":
		dst.Skills = slices.Clone(src.Skills)
	case "model":
		dst.Model = src.Model
	case "permissionMode":
		dst.PermissionMode = src.PermissionMode
	case "orchestrationTools":
		if src.OrchestrationTools == nil {
			dst.OrchestrationTools = nil
		} else {
			v := *src.OrchestrationTools
			dst.OrchestrationTools = &v
		}
	case "childPolicy":
		dst.ChildPolicy = src.ChildPolicy
	case "onRateLimit":
		dst.OnRateLimit = src.OnRateLimit
	}
}

// normalizeField is what two values are compared after: line endings and trailing
// whitespace only, so an editor's re-save never makes a text look customised.
func normalizeField(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func fieldHash(s string) string {
	sum := sha256.Sum256([]byte(normalizeField(s)))
	return hex.EncodeToString(sum[:])
}

// SyncField is one synced field of one seat as the check found it.
type SyncField struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Stored string `json:"stored"`
	Want   string `json:"want"`
}

// SeatSync is the check of one seat of the pack against the stored profile of that name.
type SeatSync struct {
	Name      string      `json:"name"`
	ID        int64       `json:"id"`
	Status    string      `json:"status"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Fields    []SyncField `json:"fields,omitempty"`
}

// Untouched reports whether no synced field of the seat is customised: every field is
// current or still what some release seeded.
func (s SeatSync) Untouched() bool {
	if s.Status != SeatOK {
		return false
	}
	for _, f := range s.Fields {
		if f.State == StateCustomised {
			return false
		}
	}
	return true
}

// HasUpdates reports whether any field of the seat is stale.
func (s SeatSync) HasUpdates() bool {
	for _, f := range s.Fields {
		if f.State == StateStale {
			return true
		}
	}
	return false
}

// CheckRoles compares every seat of the pack with the profile of its name in existing
// (read-only). The first profile of a name is the seat's, as in seeding.
func CheckRoles(existing []store.AgentProfile) []SeatSync { return checkRoles(existing, roleHistory) }

// checkRoles is CheckRoles over any history table, so tests can stage a stale value.
func checkRoles(existing []store.AgentProfile, history map[string]map[string][]string) []SeatSync {
	byName := make(map[string]store.AgentProfile, len(existing))
	ids := make(map[string]int64, len(RoleSeats))
	for _, p := range existing {
		// A plugin-owned profile of the same name must not hide the Human's own.
		if prev, dup := byName[p.Name]; !dup || (prev.Owner != nil || prev.ReadOnly) && p.Owner == nil && !p.ReadOnly {
			byName[p.Name] = p
			ids[p.Name] = p.ID
		}
	}
	out := make([]SeatSync, 0, len(RoleSeats))
	for _, seat := range RoleSeats {
		want := seat.Build(ids)
		have, ok := byName[seat.Name]
		switch {
		case !ok:
			out = append(out, SeatSync{Name: seat.Name, Status: SeatMissing})
			continue
		case have.Owner != nil || have.ReadOnly:
			out = append(out, SeatSync{Name: seat.Name, ID: have.ID, Status: SeatOwned})
			continue
		}
		s := SeatSync{Name: seat.Name, ID: have.ID, Status: SeatOK, UpdatedAt: have.UpdatedAt}
		for _, f := range syncedFields {
			stored, wantText := fieldValue(have, f.name), fieldValue(want, f.name)
			state := StateCustomised
			switch h := fieldHash(stored); {
			case h == fieldHash(wantText):
				state = StateCurrent
			case normalizeField(stored) == "":
				// An empty value equals what the first release seeded for some fields, but a
				// Human who cleared a field cannot be told from it: never overwrite it unasked.
				state = StateCustomised
			case slices.Contains(history[seat.Name][f.name], h):
				state = StateStale
			}
			s.Fields = append(s.Fields, SyncField{Name: f.name, Kind: f.kind, State: state, Stored: stored, Want: wantText})
		}
		out = append(out, s)
	}
	return out
}

// Take names what the Human asked to take from the pack regardless of its state: one field
// of a seat, or every synced field of it (Field "").
type Take struct{ Seat, Field string }

// ParseTake reads "seat" or "seat.field".
func ParseTake(s string) (Take, error) {
	seat, field, _ := strings.Cut(s, ".")
	if seat == "" {
		return Take{}, fmt.Errorf("take %q: want <seat> or <seat>.<field>", s)
	}
	if !slices.ContainsFunc(RoleSeats, func(r RoleSeat) bool { return r.Name == seat }) {
		return Take{}, fmt.Errorf("take %q: %q is not a seat of the pack", s, seat)
	}
	if field != "" && !slices.ContainsFunc(syncedFields, func(f struct{ name, kind string }) bool { return f.name == field }) {
		return Take{}, fmt.Errorf("take %q: %q is not a synced field", s, field)
	}
	return Take{Seat: seat, Field: field}, nil
}

// ApplyItem is one profile to write: the stored record the check read, the record to write
// and the fields that change.
type ApplyItem struct {
	Seat      string
	ID        int64
	Fields    []string
	Before    store.AgentProfile
	After     store.AgentProfile
	UpdatedAt time.Time // what the check read: the write is refused if the profile changed since
}

// PlanApply decides what to write. By default: the stale PROMPT fields of an untouched
// seat. A take adds a field (or every field) of a seat whatever its state: that is how a
// customised or a capability field changes. Capability fields never change without a take.
func PlanApply(checks []SeatSync, existing []store.AgentProfile, takes []Take) []ApplyItem {
	byID := make(map[int64]store.AgentProfile, len(existing))
	for _, p := range existing {
		byID[p.ID] = p
	}
	var out []ApplyItem
	for _, c := range checks {
		if c.Status != SeatOK {
			continue
		}
		var fields []string
		for _, f := range c.Fields {
			taken := slices.ContainsFunc(takes, func(t Take) bool { return t.Seat == c.Name && (t.Field == "" || t.Field == f.Name) })
			switch {
			case f.State == StateCurrent:
				continue
			case taken:
				fields = append(fields, f.Name)
			case c.Untouched() && f.Kind == KindPrompt && f.State == StateStale:
				fields = append(fields, f.Name)
			}
		}
		if len(fields) == 0 {
			continue
		}
		before := byID[c.ID]
		after := before
		want := wantProfile(c.Name, existing)
		for _, name := range fields {
			setField(&after, want, name)
		}
		out = append(out, ApplyItem{Seat: c.Name, ID: c.ID, Fields: fields, Before: before, After: after, UpdatedAt: c.UpdatedAt})
	}
	return out
}

func wantProfile(seat string, existing []store.AgentProfile) store.AgentProfile {
	ids := make(map[string]int64, len(existing))
	for _, p := range existing {
		if _, dup := ids[p.Name]; !dup {
			ids[p.Name] = p.ID
		}
	}
	for _, s := range RoleSeats {
		if s.Name == seat {
			return s.Build(ids)
		}
	}
	return store.AgentProfile{}
}

// Apply outcomes.
const (
	OutcomeUpdated          = "updated"
	OutcomeChangedMeanwhile = "changed_meanwhile"
	OutcomeDryRun           = "dry_run"
	OutcomeFailed           = "failed"
)

// ApplyOptions are the effects of ApplyRoles, injected.
type ApplyOptions struct {
	// BackupDir is where the backup goes (a timestamped directory is made inside it).
	// Required unless DryRun: with no backup nothing is written.
	BackupDir string
	Now       func() time.Time
	// Update writes one profile if it still has updatedAt expected (ErrAgentProfileChanged
	// otherwise; the daemon's profile.update with expectedUpdatedAt).
	Update func(p store.AgentProfile, expected time.Time) (store.AgentProfile, error)
	DryRun bool
}

// ApplyResult is the outcome for one profile.
type ApplyResult struct {
	Seat    string
	ID      int64
	Fields  []string
	Outcome string
	Err     error
}

// ErrNoBackupDir is returned when a write was asked for without a place to back up to.
var ErrNoBackupDir = errors.New("role pack apply: a backup directory is required")

// ApplyRoles writes items. Before the first write it saves every profile it is about to
// change, in full, as <BackupDir>/<timestamp>/<seat>-<id>.json (0600, the directory 0700);
// if that cannot be done nothing is written. A profile edited since the check is skipped
// (changed_meanwhile) and the rest proceed; a failure of one never stops the others.
// It returns the backup directory made ("" for a dry run or nothing to write).
func ApplyRoles(items []ApplyItem, opt ApplyOptions) (string, []ApplyResult, error) {
	results := make([]ApplyResult, 0, len(items))
	if len(items) == 0 {
		return "", results, nil
	}
	if opt.DryRun {
		for _, it := range items {
			results = append(results, ApplyResult{Seat: it.Seat, ID: it.ID, Fields: it.Fields, Outcome: OutcomeDryRun})
		}
		return "", results, nil
	}
	if opt.BackupDir == "" {
		return "", nil, ErrNoBackupDir
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	if fi, err := os.Lstat(opt.BackupDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("role pack apply: backup directory %s is a symlink", opt.BackupDir)
	}
	dir := filepath.Join(opt.BackupDir, now().UTC().Format("20060102T150405Z")+fmt.Sprintf("-pack-v%d", PackVersion))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("role pack apply: backup directory: %w", err)
	}
	for _, it := range items {
		data, err := json.MarshalIndent(it.Before, "", "  ")
		if err != nil {
			return "", nil, fmt.Errorf("role pack apply: back up %s: %w", it.Seat, err)
		}
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", it.Seat, it.ID)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", nil, fmt.Errorf("role pack apply: back up %s: %w", it.Seat, err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			f.Close()
			return "", nil, fmt.Errorf("role pack apply: back up %s: %w", it.Seat, err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return "", nil, fmt.Errorf("role pack apply: back up %s: %w", it.Seat, err)
		}
		if err := f.Close(); err != nil {
			return "", nil, fmt.Errorf("role pack apply: back up %s: %w", it.Seat, err)
		}
	}
	for _, it := range items {
		r := ApplyResult{Seat: it.Seat, ID: it.ID, Fields: it.Fields}
		_, err := opt.Update(it.After, it.UpdatedAt)
		switch {
		case err == nil:
			r.Outcome = OutcomeUpdated
		case errors.Is(err, store.ErrAgentProfileChanged):
			r.Outcome = OutcomeChangedMeanwhile
		default:
			r.Outcome, r.Err = OutcomeFailed, err
		}
		results = append(results, r)
	}
	return dir, results, nil
}
