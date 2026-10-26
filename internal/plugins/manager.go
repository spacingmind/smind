package plugins

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
	"github.com/spacingmind/smind/internal/store"
)

// Options configures a Manager.
type Options struct {
	// Home is $SMIND_HOME; plugins live under Home/plugins.
	Home string
	// Store holds the install state.
	Store *store.Store
	// Version is the running daemon's version, for engines.smind.
	Version string
	// Enabled reports the global switch (config plugins.enabled). It is
	// read on every operation, fails closed (nil means off) and is never
	// settable through this package.
	Enabled func() bool
	// Limits overrides DefaultLimits (tests).
	Limits Limits
	// Settings supplies plugin setting values for ${setting.KEY} in MCP
	// env and headers (nil: none are set).
	Settings SettingsProvider
	// ValidateProfile checks a contributed profile against the provider
	// vocabulary (nil: structural checks only).
	ValidateProfile func(store.AgentProfile) error
	// UserProfileNames lists the user's profile names, so a contributed
	// profile never shadows one (the user's wins and a conflict is noted).
	UserProfileNames func() []string
	// Host, when set, turns on process hosting: enabled plugins that declare
	// `process` are run (ADR-0042 Wave B). nil keeps every plugin inert. Gate
	// and OnChange are set by the Manager.
	Host *host.Options
}

// Manager installs and tracks plugins. All mutators serialize on one lock; a
// failed install, update or reload leaves the last good version in place.
type Manager struct {
	home    string
	st      *store.Store
	version string
	enabled func() bool
	lim     Limits

	settings         SettingsProvider
	validateProfile  func(store.AgentProfile) error
	userProfileNames func() []string
	cache            contribCache
	proc             *hostState

	mu sync.Mutex
	// failAt, when set (tests only), is called at named points of an install
	// or update; a non-nil result aborts the operation there.
	failAt func(point string) error
}

// New returns a Manager. It creates nothing on disk until the first install.
func New(o Options) *Manager {
	lim := o.Limits
	if lim == (Limits{}) {
		lim = DefaultLimits
	}
	m := &Manager{home: o.Home, st: o.Store, version: o.Version, enabled: o.Enabled, lim: lim,
		settings: o.Settings, validateProfile: o.ValidateProfile, userProfileNames: o.UserProfileNames}
	if o.Host != nil {
		m.newHost(*o.Host)
	}
	return m
}

// GlobalEnabled reports the plugins.enabled switch.
func (m *Manager) GlobalEnabled() bool { return m.enabled != nil && m.enabled() }

func (m *Manager) root() string         { return filepath.Join(m.home, "plugins") }
func (m *Manager) dir(id string) string { return filepath.Join(m.root(), id) }
func (m *Manager) stagingDir() string   { return filepath.Join(m.root(), ".staging") }
func (m *Manager) pendingDir() string   { return filepath.Join(m.root(), ".pending") }
func (m *Manager) trashDir() string     { return filepath.Join(m.root(), ".trash") }

func (m *Manager) point(name string) error {
	if m.failAt != nil {
		return m.failAt(name)
	}
	return nil
}

func (m *Manager) requireSwitch() error {
	if !m.GlobalEnabled() {
		return newErr(CodeSwitchOff, "plugins are turned off: a human sets `plugins.enabled: true` in config.yaml")
	}
	return nil
}

// Info is the wire and CLI view of an installed plugin.
type Info struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	SourceKind string `json:"sourceKind"`
	Hash       string `json:"hash"`
	Enabled    bool   `json:"enabled"`
	// Active is true only when the plugin is enabled AND the global switch is
	// on: the only state in which a contribution may be loaded or called.
	Active    bool     `json:"active"`
	Consent   Consent  `json:"consent"`
	Consented bool     `json:"consented"`
	Pending   *Pending `json:"pending,omitempty"`
	LastError string   `json:"lastError,omitempty"`
	// Conflicts lists what this plugin contributed that the host did not
	// apply (a user row with the same name, a setting with no value).
	Conflicts []Conflict `json:"conflicts,omitempty"`
	// Process is the plugin process's status while one is supervised
	// (starting, running, restarting, failed); absent for a data-only plugin,
	// a disabled one and while the global switch is off.
	Process     *host.Status `json:"process,omitempty"`
	InstalledAt time.Time    `json:"installedAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// Pending is an update waiting for the Human's re-consent.
type Pending struct {
	Consent Consent `json:"consent"`
}

// Listing is plugin.list's result: the global switch and every plugin.
type Listing struct {
	GlobalEnabled bool   `json:"globalEnabled"`
	Plugins       []Info `json:"plugins"`
}

func (m *Manager) info(p store.Plugin) Info { return m.infoG(p, m.GlobalEnabled()) }

// infoG is info with the global switch already read (it is a file read).
func (m *Manager) infoG(p store.Plugin, global bool) Info {
	var c Consent
	_ = json.Unmarshal([]byte(p.Capabilities), &c)
	in := Info{
		ID: p.ID, Name: p.Name, Version: p.Version, Source: p.Source, SourceKind: p.SourceKind, Hash: p.Hash,
		Enabled: p.Enabled, Active: p.Enabled && global, Consent: c,
		Consented: p.ConsentHash != "" && p.ConsentHash == c.Fingerprint, LastError: p.LastError,
		InstalledAt: p.InstalledAt, UpdatedAt: p.UpdatedAt,
	}
	if st, ok := m.ProcessStatus(p.ID); ok && p.Enabled && global {
		in.Process = &st
		if st.State == host.StateFailed && in.LastError == "" {
			in.LastError = "process failed: " + st.LastError
		}
	}
	if p.Pending != "" {
		var pc Consent
		if json.Unmarshal([]byte(p.Pending), &pc) == nil {
			in.Pending = &Pending{Consent: pc}
		}
	}
	return in
}

// List returns every plugin and the global switch. With the switch off no
// plugin reports Active.
func (m *Manager) List() (Listing, error) {
	l, err := m.list()
	if err != nil {
		return l, err
	}
	if confl := m.derive().conflicts; len(confl) > 0 {
		for i := range l.Plugins {
			for _, c := range confl {
				if c.Plugin == l.Plugins[i].ID {
					l.Plugins[i].Conflicts = append(l.Plugins[i].Conflicts, c)
				}
			}
		}
	}
	return l, nil
}

// list is List without the contribution conflicts (derive builds on it).
func (m *Manager) list() (Listing, error) {
	rows, err := m.st.ListPlugins()
	if err != nil {
		return Listing{}, err
	}
	global := m.GlobalEnabled()
	out := Listing{GlobalEnabled: global, Plugins: make([]Info, 0, len(rows))}
	for _, r := range rows {
		out.Plugins = append(out.Plugins, m.infoG(r, global))
	}
	return out, nil
}

// Active returns the plugins whose contributions may be loaded: none while the
// global switch is off.
func (m *Manager) Active() []Info {
	if !m.GlobalEnabled() {
		return nil
	}
	l, err := m.list()
	if err != nil {
		return nil
	}
	var out []Info
	for _, in := range l.Plugins {
		if in.Active {
			out = append(out, in)
		}
	}
	return out
}

func (m *Manager) row(id string) (store.Plugin, error) {
	p, err := m.st.GetPlugin(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Plugin{}, newErr(CodeNotFound, "no plugin %q is installed", id)
	}
	return p, err
}

// Get returns one plugin.
func (m *Manager) Get(id string) (Info, error) {
	p, err := m.row(id)
	if err != nil {
		return Info{}, err
	}
	in := m.info(p)
	for _, c := range m.derive().conflicts {
		if c.Plugin == id {
			in.Conflicts = append(in.Conflicts, c)
		}
	}
	return in, nil
}

// staged is a validated candidate: its files are in work, not yet in place.
type staged struct {
	work string // removed by cleanup
	tree string // the plugin's files (the source dir itself for a link)
	man  *Manifest
	hash string
	cons Consent
}

func (s *staged) cleanup() { os.RemoveAll(s.work) }

func (m *Manager) newWork() (string, error) {
	if err := os.MkdirAll(m.stagingDir(), 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(m.stagingDir(), "s-")
}

// tidy removes the staging and root directories when empty, so a failed first
// install leaves the home exactly as it was.
func (m *Manager) tidy() {
	_ = os.Remove(m.stagingDir())
	_ = os.Remove(m.root())
}

// stage fetches src into a fresh work directory, applies every safety check
// and validates the manifest. Nothing outside the work directory is touched.
func (m *Manager) stage(ctx context.Context, src Source) (*staged, error) {
	work, err := m.newWork()
	if err != nil {
		return nil, err
	}
	st := &staged{work: work}
	fail := func(err error) (*staged, error) {
		st.cleanup()
		m.tidy()
		return nil, err
	}
	tree := filepath.Join(work, "tree")
	if src.Kind != SourceLink {
		if err := os.Mkdir(tree, 0o700); err != nil {
			return fail(err)
		}
	}
	switch src.Kind {
	case SourceDir:
		st.hash, err = CopyDir(src.Spec, tree, m.lim)
	case SourceArchive:
		ar := filepath.Join(work, "archive.bin")
		if err = fetchArchive(ctx, src, ar); err == nil {
			st.hash, err = ExtractArchive(ar, tree, m.lim)
		}
	case SourceGit:
		co := filepath.Join(work, "checkout")
		if err = os.Mkdir(co, 0o700); err == nil {
			if err = fetchGit(ctx, src, work, co); err == nil {
				st.hash, err = CopyDir(co, tree, m.lim)
			}
		}
	case SourceLink:
		var real string
		if real, err = filepath.EvalSymlinks(src.Spec); err == nil {
			tree = real
		}
	}
	if err != nil {
		return fail(err)
	}
	st.tree = tree
	raw, err := readManifestFile(tree)
	if err != nil {
		return fail(err)
	}
	if src.Kind == SourceLink {
		sum := sha256.Sum256(raw)
		st.hash = hex.EncodeToString(sum[:])
	}
	if st.man, err = ParseManifest(raw, m.version); err != nil {
		return fail(err)
	}
	if err := checkDeclared(tree, st.man, src.Kind != SourceLink); err != nil {
		return fail(err)
	}
	if err := st.man.loadContributions(tree); err != nil {
		return fail(err)
	}
	st.cons = BuildConsent(st.man, src, st.hash)
	return st, nil
}

// readManifestFile reads the manifest under root, refusing a symlink and
// anything over the cap before it is read.
func readManifestFile(root string) ([]byte, error) {
	p := filepath.Join(root, ManifestFile)
	lst, err := os.Lstat(p)
	if err != nil {
		return nil, newErr(CodeBadManifest, "%s not found at the plugin root: put it at the top of the source (not in a subfolder)", ManifestFile)
	}
	if !lst.Mode().IsRegular() {
		return nil, newErr(CodeBadManifest, "%s must be a regular file, not a link", ManifestFile)
	}
	if lst.Size() > MaxManifestBytes {
		return nil, &ManifestError{Errs: []*FieldError{{Field: "(file)", Problem: fmt.Sprintf("%s is %d bytes, over the %d byte cap", ManifestFile, lst.Size(), MaxManifestBytes), Fix: "shrink the manifest"}}}
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// checkDeclared verifies each manifest-declared path exists inside root as a
// regular file or directory that does not resolve through a symlink out of it,
// and (for a copied tree) that nothing claims the reserved data/ name.
func checkDeclared(root string, man *Manifest, copied bool) error {
	if copied {
		if _, err := os.Lstat(filepath.Join(root, "data")); err == nil {
			return newErr(CodeUnsafeEntry, "the plugin contains a top-level %q entry; data/ is reserved for the host", "data")
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, p := range man.DeclaredPaths() {
		full := filepath.Join(realRoot, filepath.FromSlash(p))
		real, err := filepath.EvalSymlinks(full)
		if err != nil {
			return &ManifestError{Errs: []*FieldError{{Field: p, Problem: "declared path does not exist in the plugin", Fix: "add the file or remove the declaration"}}}
		}
		if rel, err := filepath.Rel(realRoot, real); err != nil || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
			return newErr(CodeLinkRejected, "declared path %q resolves outside the plugin through a link", p)
		}
	}
	return nil
}

func (m *Manager) save(c Consent) string {
	b, _ := json.Marshal(c)
	return string(b)
}

// Install copies, extracts or checks out arg (a directory, git+<url>@<sha> or
// <archive>#sha256=<hash>), validates it and records it DISABLED. Enabling is a
// separate, confirmed step.
func (m *Manager) Install(ctx context.Context, arg string) (Info, error) {
	src, err := ParseSource(arg, false)
	if err != nil {
		return Info{}, err
	}
	return m.install(ctx, src)
}

// Link records a directory in place (never copied). The plugin's code stays
// where the developer keeps it; remove never deletes it.
func (m *Manager) Link(ctx context.Context, path string) (Info, error) {
	src, err := ParseSource(path, true)
	if err != nil {
		return Info{}, err
	}
	return m.install(ctx, src)
}

func (m *Manager) install(ctx context.Context, src Source) (Info, error) {
	if err := m.requireSwitch(); err != nil {
		return Info{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stage(ctx, src)
	if err != nil {
		return Info{}, err
	}
	defer func() { st.cleanup(); m.tidy() }()
	id := st.man.ID
	if _, err := m.st.GetPlugin(id); err == nil {
		return Info{}, newErr(CodeInstalled, "plugin %q is already installed: use `smind plugin update %s`", id, id)
	}
	if err := m.point("staged"); err != nil {
		return Info{}, err
	}
	dst := m.dir(id)
	if err := m.claimDir(dst, st.tree, src.Kind == SourceLink); err != nil {
		return Info{}, err
	}
	undo := func() { m.removeKeepingData(dst) }
	if err := m.point("published"); err != nil {
		undo()
		return Info{}, err
	}
	p, err := m.st.InsertPlugin(store.Plugin{
		ID: id, Name: st.man.Name, Version: st.man.Version, SourceKind: string(src.Kind),
		Source: src.String(), Hash: st.hash, Capabilities: m.save(st.cons),
	})
	if err != nil {
		undo()
		if errors.Is(err, store.ErrPluginExists) {
			return Info{}, newErr(CodeInstalled, "plugin %q is already installed", id)
		}
		return Info{}, err
	}
	return m.info(p), nil
}

// claimDir puts a staged tree at dst (atomic rename for a copy; just the data
// directory for a link). A directory already there without a row is only
// accepted when it holds nothing but a kept data/ directory.
func (m *Manager) claimDir(dst, tree string, link bool) error {
	if err := os.MkdirAll(m.root(), 0o755); err != nil {
		return err
	}
	var keepData string
	if ents, err := os.ReadDir(dst); err == nil {
		for _, e := range ents {
			if e.Name() != "data" {
				return newErr(CodeLayout, "%s already exists and is not a kept data directory: remove it by hand if it is stale", dst)
			}
		}
		if len(ents) == 1 {
			keepData = filepath.Join(dst, "data")
			if st, err := os.Lstat(keepData); err != nil || !st.IsDir() {
				return newErr(CodeLayout, "%s is not a plain directory: remove it by hand if it is stale", keepData)
			}
		}
	}
	if link {
		return os.MkdirAll(filepath.Join(dst, "data"), 0o700)
	}
	if keepData != "" {
		if err := os.Rename(keepData, filepath.Join(tree, "data")); err != nil {
			return err
		}
	} else if err := os.Mkdir(filepath.Join(tree, "data"), 0o700); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(tree, dst)
}

// removeKeepingData is install rollback: it deletes what a failed install put at
// dst but never a data/ directory that existed before.
func (m *Manager) removeKeepingData(dst string) {
	ents, err := os.ReadDir(dst)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.Name() != "data" {
			os.RemoveAll(filepath.Join(dst, e.Name()))
		}
	}
	if d, err := os.ReadDir(filepath.Join(dst, "data")); err == nil && len(d) == 0 {
		os.Remove(filepath.Join(dst, "data"))
		os.Remove(dst)
	}
}

// loadInstalled re-reads and re-validates an installed plugin's manifest from
// disk: engines and api against this daemon, declared files present, and for
// a copied plugin the files unchanged since install.
func (m *Manager) loadInstalled(p store.Plugin) (*Manifest, string, error) {
	root := m.dir(p.ID)
	link := p.SourceKind == string(SourceLink)
	if link {
		root = p.Source
	}
	raw, err := readManifestFile(root)
	if err != nil {
		return nil, "", err
	}
	man, err := ParseManifest(raw, m.version)
	if err != nil {
		return nil, "", err
	}
	if man.ID != p.ID {
		return nil, "", newErr(CodeBadManifest, "manifest id is now %q, installed as %q", man.ID, p.ID)
	}
	hash := p.Hash
	if link {
		sum := sha256.Sum256(raw)
		hash = hex.EncodeToString(sum[:])
	} else if hash, err = treeHashOf(root); err != nil || hash != p.Hash {
		return nil, "", newErr(CodeBadManifest, "the installed files changed since install (hash mismatch): reinstall or update the plugin")
	}
	if err := checkDeclaredInstalled(root, man, link); err != nil {
		return nil, "", err
	}
	if err := man.loadContributions(root); err != nil {
		return nil, "", err
	}
	return man, hash, nil
}

func checkDeclaredInstalled(root string, man *Manifest, link bool) error {
	if link {
		return checkDeclared(root, man, false)
	}
	// data/ legitimately exists in an installed copy.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, p := range man.DeclaredPaths() {
		if _, err := os.Lstat(filepath.Join(realRoot, filepath.FromSlash(p))); err != nil {
			return &ManifestError{Errs: []*FieldError{{Field: p, Problem: "declared path does not exist in the plugin", Fix: "reinstall the plugin"}}}
		}
	}
	return nil
}

func (m *Manager) setError(p store.Plugin, err error) {
	p.LastError = err.Error()
	_ = m.st.UpdatePlugin(p)
}

// Enable turns a plugin on. confirm must equal the fingerprint of the consent
// summary the Human was shown (Info.Consent.Fingerprint): the daemon never
// enables on a bare id, and an update that changed capabilities invalidates
// an older confirmation.
func (m *Manager) Enable(id, confirm string) (Info, error) {
	if err := m.requireSwitch(); err != nil {
		return Info{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return Info{}, err
	}
	var c Consent
	_ = json.Unmarshal([]byte(p.Capabilities), &c)
	if confirm == "" || confirm != c.Fingerprint {
		return Info{}, newErr(CodeConsent, "enabling %q needs confirmation of what it will do: review `smind plugin show %s` and confirm (--yes)", id, id)
	}
	man, _, err := m.loadInstalled(p)
	if err != nil {
		m.setError(p, err)
		return Info{}, err
	}
	p.Enabled, p.ConsentHash, p.LastError = true, c.Fingerprint, ""
	if err := m.st.UpdatePlugin(p); err != nil {
		return Info{}, err
	}
	m.syncOne(p, man, true)
	return m.info(p), nil
}

// Disable turns a plugin off. It needs no confirmation and works while the
// global switch is off.
func (m *Manager) Disable(id string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return Info{}, err
	}
	p.Enabled = false
	if err := m.st.UpdatePlugin(p); err != nil {
		return Info{}, err
	}
	if m.proc != nil {
		m.proc.h.Stop(id) // returns once the process tree is gone
	}
	return m.info(p), nil
}

// Remove deletes the plugin's row, its files under plugins/<id> and any
// pending update, and its data/ directory unless keepData. A linked source is
// never touched: only the host-owned plugins/<id> directory is removed.
func (m *Manager) Remove(id string, keepData bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return err
	}
	if m.proc != nil {
		m.proc.h.Stop(id) // before the files go: nothing may run from a deleted directory
		m.proc.h.Forget(id)
	}
	if err := m.st.DeletePlugin(id); err != nil {
		return err
	}
	// Settings can hold secrets: they go with the plugin even with keepData
	// (that flag keeps the data/ directory only).
	if err := m.st.DeletePluginSettings(id); err != nil {
		slog.Warn("plugins: deleting settings", "plugin", id, "err", err)
	}
	_ = os.RemoveAll(filepath.Join(m.pendingDir(), id))
	_ = os.Remove(m.pendingDir())
	dst := m.dir(id)
	if !keepData {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("plugin %q removed, but deleting %s failed: %w", id, dst, err)
		}
		return nil
	}
	ents, err := os.ReadDir(dst)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if e.Name() == "data" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
			return fmt.Errorf("plugin %q removed, but deleting %s failed: %w", id, filepath.Join(dst, e.Name()), err)
		}
	}
	_ = p
	return nil
}

// UpdateResult says what Update did.
type UpdateResult struct {
	Applied bool `json:"applied"`
	Pending bool `json:"pending"`
	Info    Info `json:"plugin"`
}

// Update stages a new version from arg (empty: re-read the recorded directory
// source). If it changes the declared capabilities, process command, MCP/ACP
// commands or hook events it stays PENDING and the previous version keeps
// serving until the Human applies it (apply true, with no arg, or arg plus
// apply). A failure leaves the last good version active and records the
// error on it.
func (m *Manager) Update(ctx context.Context, id, arg string, apply bool) (UpdateResult, error) {
	if err := m.requireSwitch(); err != nil {
		return UpdateResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return UpdateResult{}, err
	}
	if p.SourceKind == string(SourceLink) {
		return UpdateResult{}, newErr(CodeBadSource, "%q is linked: edit the directory and run `smind plugin reload %s`", id, id)
	}
	if arg == "" && apply {
		return m.applyPending(p)
	}
	var src Source
	if arg == "" {
		if p.SourceKind != string(SourceDir) {
			return UpdateResult{}, newErr(CodeBadSource, "%q was installed from a pinned %s source: pass the new pinned source to update", id, p.SourceKind)
		}
		src = Source{Kind: SourceDir, Spec: p.Source}
	} else if src, err = ParseSource(arg, false); err != nil {
		return UpdateResult{}, err
	}
	st, err := m.stage(ctx, src)
	if err != nil {
		m.setError(p, err)
		return UpdateResult{}, err
	}
	defer func() { st.cleanup(); m.tidy() }()
	if st.man.ID != id {
		err := newErr(CodeBadManifest, "the new version declares id %q, not %q", st.man.ID, id)
		m.setError(p, err)
		return UpdateResult{}, err
	}
	if err := m.point("staged"); err != nil {
		m.setError(p, err)
		return UpdateResult{}, err
	}
	var old Consent
	_ = json.Unmarshal([]byte(p.Capabilities), &old)
	if st.cons.Fingerprint != old.Fingerprint && !apply {
		dst := filepath.Join(m.pendingDir(), id)
		if err := os.MkdirAll(m.pendingDir(), 0o700); err != nil {
			return UpdateResult{}, err
		}
		if err := os.RemoveAll(dst); err != nil {
			return UpdateResult{}, err
		}
		if err := os.Rename(st.tree, dst); err != nil {
			m.setError(p, err)
			return UpdateResult{}, err
		}
		p.Pending, p.LastError = m.save(st.cons), ""
		if err := m.st.UpdatePlugin(p); err != nil {
			os.RemoveAll(dst)
			return UpdateResult{}, err
		}
		return UpdateResult{Pending: true, Info: m.info(p)}, nil
	}
	return m.swapInRestart(p, st.tree, st.cons, src)
}

func (m *Manager) applyPending(p store.Plugin) (UpdateResult, error) {
	if p.Pending == "" {
		return UpdateResult{}, newErr(CodeNoPending, "%q has no pending update", p.ID)
	}
	var pc Consent
	if err := json.Unmarshal([]byte(p.Pending), &pc); err != nil {
		return UpdateResult{}, err
	}
	tree := filepath.Join(m.pendingDir(), p.ID)
	if _, err := os.Stat(tree); err != nil {
		return UpdateResult{}, newErr(CodeNoPending, "the staged files of the pending update are gone: run update again")
	}
	// The staged files must still be what was staged.
	if h, err := treeHashOf(tree); err != nil || h != pc.Hash {
		return UpdateResult{}, newErr(CodeHashMismatch, "the staged update changed on disk: run update again")
	}
	src := Source{Kind: SourceKind(pc.SourceKind), Spec: pc.Source}
	return m.swapInRestart(p, tree, pc, src)
}

// swapInRestart is swapIn followed by a restart of the plugin's process on the
// new files.
func (m *Manager) swapInRestart(p store.Plugin, tree string, c Consent, src Source) (UpdateResult, error) {
	res, err := m.swapIn(p, tree, c, src)
	if err == nil && res.Applied {
		m.restartAfterSwap(p.ID)
		if np, gerr := m.st.GetPlugin(p.ID); gerr == nil {
			res.Info = m.info(np)
		}
	}
	return res, err
}

// swapIn replaces the live files with tree and the row with the new version.
// The Human has consented (an unchanged fingerprint or an explicit apply). The
// row is written first and rolled back if the directory swap fails.
func (m *Manager) swapIn(p store.Plugin, tree string, c Consent, src Source) (UpdateResult, error) {
	prev := p
	p.Name, p.Version, p.Hash, p.Capabilities, p.Pending, p.LastError = c.Name, c.Version, c.Hash, m.save(c), "", ""
	p.Source, p.SourceKind = c.Source, c.SourceKind
	if src.Kind == SourceDir {
		p.Source = src.Spec
	}
	if p.Enabled || p.ConsentHash != "" {
		p.ConsentHash = c.Fingerprint // the Human confirmed this update
	}
	if err := m.point("swap"); err != nil {
		m.setError(prev, err)
		return UpdateResult{}, err
	}
	if err := m.st.UpdatePlugin(p); err != nil {
		return UpdateResult{}, err
	}
	if err := m.swapDirs(p.ID, tree); err != nil {
		_ = m.st.UpdatePlugin(prev)
		m.setError(prev, err)
		return UpdateResult{}, err
	}
	_ = os.RemoveAll(filepath.Join(m.pendingDir(), p.ID))
	_ = os.Remove(m.pendingDir())
	return UpdateResult{Applied: true, Info: m.info(p)}, nil
}

// swapDirs atomically replaces plugins/<id> with tree, carrying data/ across.
func (m *Manager) swapDirs(id, tree string) error {
	live := m.dir(id)
	if err := os.MkdirAll(m.trashDir(), 0o700); err != nil {
		return err
	}
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	old := filepath.Join(m.trashDir(), id+"-"+hex.EncodeToString(rnd[:]))
	liveData, treeData := filepath.Join(live, "data"), filepath.Join(tree, "data")
	hadData := false
	if _, err := os.Stat(liveData); err == nil {
		if err := os.Rename(liveData, treeData); err != nil {
			return err
		}
		hadData = true
	} else if err := os.MkdirAll(treeData, 0o700); err != nil {
		return err
	}
	restoreData := func() {
		if hadData {
			_ = os.Rename(treeData, liveData)
		}
	}
	if err := os.Rename(live, old); err != nil {
		restoreData()
		return err
	}
	if err := os.Rename(tree, live); err != nil {
		_ = os.Rename(old, live)
		restoreData()
		return err
	}
	_ = os.RemoveAll(old)
	_ = os.Remove(m.trashDir())
	return nil
}

// Reload re-reads and re-validates an installed plugin. A failure keeps the
// last good state and records the error. For a linked plugin a changed
// capability set disables the plugin until the Human re-confirms, because the
// old files are gone and cannot keep serving.
func (m *Manager) Reload(id string) (Info, error) {
	if err := m.requireSwitch(); err != nil {
		return Info{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.row(id)
	if err != nil {
		return Info{}, err
	}
	man, hash, err := m.loadInstalled(p)
	if err != nil {
		m.setError(p, err)
		return Info{}, err
	}
	var old Consent
	_ = json.Unmarshal([]byte(p.Capabilities), &old)
	src := Source{Kind: SourceKind(p.SourceKind), Spec: p.Source}
	if src.Kind != SourceDir && src.Kind != SourceLink {
		src = Source{Kind: SourceKind(p.SourceKind), Spec: old.Source}
	}
	cons := BuildConsent(man, src, hash)
	cons.Source = old.Source
	if cons.Source == "" {
		cons.Source = p.Source
	}
	p.Name, p.Version, p.Hash, p.LastError = man.Name, man.Version, hash, ""
	p.Capabilities = m.save(cons)
	if cons.Fingerprint != old.Fingerprint {
		p.Enabled, p.ConsentHash = false, ""
		p.LastError = "declared capabilities changed on reload: review and enable again"
	}
	if err := m.st.UpdatePlugin(p); err != nil {
		return Info{}, err
	}
	m.syncOne(p, man, true) // a failed reload returned above: the last good process keeps running
	return m.info(p), nil
}

// Start reconciles disk and database at daemon startup: leftovers of an
// interrupted install or swap are deleted, and every enabled plugin is
// re-validated (a plugin that no longer validates, or whose files changed
// since the Human consented, is disabled with its error recorded). With the
// global switch off nothing is touched beyond the leftovers.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range []string{m.stagingDir(), m.trashDir()} {
		if ents, err := os.ReadDir(d); err == nil {
			for _, e := range ents {
				_ = os.RemoveAll(filepath.Join(d, e.Name()))
			}
			_ = os.Remove(d)
		}
	}
	if !m.GlobalEnabled() {
		return
	}
	rows, err := m.st.ListPlugins()
	if err != nil {
		return
	}
	for _, p := range rows {
		if !p.Enabled {
			continue
		}
		var c Consent
		_ = json.Unmarshal([]byte(p.Capabilities), &c)
		man, _, err := m.loadInstalled(p)
		if err == nil && p.ConsentHash != c.Fingerprint {
			err = newErr(CodeConsent, "no valid consent is recorded for the installed capabilities")
		}
		if err != nil {
			p.Enabled, p.LastError = false, err.Error()
			_ = m.st.UpdatePlugin(p)
			continue
		}
		m.syncOne(p, man, false)
	}
}
