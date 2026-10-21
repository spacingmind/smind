package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/sandbox"
)

// ProjectProbe materialises skills (the active set plus ONE proposed
// candidate, already rendered by the caller) for a probe arm. It is the only
// path by which a not-yet-approved skill reaches an agent (ADR-0037 §2): att
// must be a valid sandbox attestation at now, checked before anything is
// created. Production delivery uses Project and never this.
//
// The directory holds two layouts of the same skills: .claude/skills/<name>/
// SKILL.md (the form loads.Match reads) and a plugin "smindprobe"
// (.claude-plugin/plugin.json + skills/<name>/SKILL.md), which is what makes
// the CLI load them in an arm run with --setting-sources= (measured: a skill
// under --add-dir is not discovered then, a --plugin-dir plugin is). The hash
// covers every file.
//
// root must be a probe-only root (e.g. <home>/learning/probe-projections), so
// production GC and Remove never touch it. The build is Project's: content
// addressed, read-only, the returned Hash is what the service records on the
// arm's run. Empty skills returns the zero Projection.
//
// Project's GC keeps only the newest two hash directories per workspace under
// root, so a caller running more than two distinct projections of one
// workspace at once must give each its own root or serialise them.
func ProjectProbe(att sandbox.Attestation, now time.Time, root string, workspaceID int64, skills []Skill) (Projection, error) {
	if err := att.Require(now); err != nil {
		return Projection{}, fmt.Errorf("projection: probe projection refused: %w", err)
	}
	if len(skills) == 0 {
		return Projection{}, nil
	}
	if workspaceID <= 0 {
		return Projection{}, fmt.Errorf("projection: invalid workspace id %d", workspaceID)
	}
	files, _, err := render(skills)
	if err != nil {
		return Projection{}, err
	}
	plugin := make(map[string][]byte, len(files))
	for rel, data := range files { // .claude/skills/<name>/SKILL.md -> skills/<name>/SKILL.md
		plugin[filepath.Join("skills", filepath.Base(filepath.Dir(rel)), "SKILL.md")] = data
	}
	for rel, data := range plugin {
		files[rel] = data
	}
	files[filepath.Join(".claude-plugin", "plugin.json")] = []byte(probePluginJSON)
	return projectFiles(root, workspaceID, files, hashFiles(files))
}

// ProbePluginName is the plugin the probe skills are delivered as; the CLI
// lists them as <ProbePluginName>:<skill>.
const ProbePluginName = "smindprobe"

// probePluginJSON is the plugin manifest: fixed bytes, so the hash is stable.
const probePluginJSON = `{"name":"smindprobe","version":"0.0.1","description":"smind probe skills"}` + "\n"

// hashFiles is a content hash over every path and its bytes, sorted.
func hashFiles(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(files[p])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
