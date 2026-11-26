package permpolicy

import (
	"regexp"
	"strings"
)

// The never-list (docs/plans/completed/never-list-floor.md): deny-only floor rule
// `no-catastrophic` for commands that wipe the filesystem root or the home,
// format or zero a device, fork-bomb the machine or power it off. It is
// compiled in like every floor rule. It only adds what `no-destructive-outside`
// does not already deny: glob targets (`rm -rf ~/*`), devices named as plain
// words (`mkfs /dev/sda`), the Windows disk tools, fork bombs, and every one
// of those inside a command runner (`su -c`, `ssh host '...'`, `eval`) or a
// shell fed text (`echo '...' | sh`), which no other floor rule unwraps.

const catastrophicID = "no-catastrophic"

// maxRunnerDepth caps how many times a runner's payload is itself re-judged.
const maxRunnerDepth = 3

// catastrophicRules judges one segment: its redirects, the command itself,
// and the commands a runner or shell inside it would run.
func (fs *floorSeg) catastrophicRules() (FloorHit, bool) {
	if fs.catastrophicDeep(0) {
		return floorHit(catastrophicID)
	}
	return FloorHit{}, false
}

// catastrophicDeep is catastrophic() plus the payload of a command runner or
// `shell -c`, re-split and judged as commands.
func (fs *floorSeg) catastrophicDeep(depth int) bool {
	if fs.catastrophicRedirect() {
		return true
	}
	if len(fs.argv) == 0 {
		return false
	}
	if fs.catastrophic() {
		return true
	}
	if depth >= maxRunnerDepth {
		return false
	}
	name := fs.name()
	if !commandRunners[name] && !shellNames[name] {
		return false
	}
	args := fs.argv[1:]
	// A script handed over as one word: `su -c 'rm -rf /'`, `eval 'mkfs /dev/sda'`.
	nf := nonFlags(args)
	for i, a := range args {
		// One word is a script only where a script is expected: after -c, for eval,
		// or as ssh's command word (`ssh host reboot`, not `ssh reboot`).
		oneWord := (i > 0 && args[i-1] == "-c") || name == "eval" ||
			(name == "ssh" && len(nf) == 2 && i == len(args)-1)
		if (oneWord || strings.ContainsAny(a, " \t\n;&|")) && fs.textCatastrophic(a, depth+1) {
			return true
		}
	}
	// A script fed on stdin by a here-string: `at now <<< 'rm -rf /'`.
	for _, rd := range fs.seg.Redirects {
		if rd.Op == "<<<" && fs.textCatastrophic(rd.Target, depth+1) {
			return true
		}
	}
	// A command handed over as words: `ssh host rm -rf /`, `nsenter -t 1 mkfs /dev/sda`.
	for i := range args {
		sub := *fs
		sub.argv = args[i:]
		sub.seg.Redirects = nil
		sub.viaSuffix = true
		if sub.catastrophicDeep(depth + 1) {
			return true
		}
	}
	return false
}

// textCatastrophic re-splits text and judges every simple command in it.
func (fs *floorSeg) textCatastrophic(text string, depth int) bool {
	p := splitCommand(text, fs.pl.windows)
	cwd := fs.cwd
	for _, seg := range p.Segments {
		sub := newFloorSeg(fs.req, seg, cwd)
		if sub.catastrophicDeep(depth) {
			return true
		}
		cwd = sub.cwdAfter()
	}
	return false
}

// shellFedCatastrophic judges the text a bare shell would read: the words of
// echo/printf segments, here-strings, and every raw line (a heredoc body).
func shellFedCatastrophic(req Request, segs []*floorSeg, raw string) bool {
	if !bareShellFed(segs) {
		return false
	}
	var texts []string
	for _, fs := range segs {
		for _, rd := range fs.seg.Redirects {
			if rd.Op == "<<<" {
				texts = append(texts, rd.Target)
			}
		}
		if len(fs.argv) < 2 {
			continue
		}
		switch fs.name() {
		case "echo", "printf":
			texts = append(texts, strings.Join(fs.argv[1:], " "))
			texts = append(texts, fs.argv[1:]...)
		}
	}
	texts = append(texts, strings.Split(raw, "\n")...)
	// The three-index slice bounds the range to the texts so far: the appends below are not re-read.
	for _, t := range texts[:len(texts):len(texts)] {
		if strings.Contains(t, `\n`) { // printf 'cmd\n' | sh
			texts = append(texts, strings.Split(t, `\n`)...)
		}
	}
	probe := newFloorSeg(req, Segment{}, req.Cwd)
	for _, t := range texts {
		if strings.TrimSpace(t) != "" && probe.textCatastrophic(t, maxRunnerDepth-1) {
			return true
		}
	}
	return false
}

// catastrophicRedirect reports a redirect that writes to a block device.
func (fs *floorSeg) catastrophicRedirect() bool {
	for _, rd := range fs.seg.Redirects {
		if strings.Contains(rd.Op, ">") && fs.isDevice(rd.Target) {
			return true
		}
	}
	return false
}

// recursiveFlag reports an option that makes the command act on a whole tree.
func recursiveFlag(args []string, win bool) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		switch {
		case a == "--recursive", shortCluster(a, "rR"):
			return true
		case win && (strings.HasPrefix(strings.ToLower(a), "-r") || strings.EqualFold(a, "/s")):
			return true
		}
	}
	return false
}

// windowsDiskCmds are command names unique to Windows tooling, matched
// whatever the flavour (a PowerShell copy runs on any OS).
var windowsDiskCmds = map[string]bool{
	"format-volume": true, "clear-disk": true, "remove-partition": true, "initialize-disk": true,
	"diskpart": true, "stop-computer": true, "restart-computer": true,
}

// powerCmds stop the machine, and with it the daemon and every other run.
var powerCmds = map[string]bool{"shutdown": true, "reboot": true, "halt": true, "poweroff": true}

// diskutilDestroys are the diskutil verbs that erase, format or repartition.
var diskutilDestroys = map[string]bool{
	"erasedisk": true, "erasevolume": true, "secureerase": true, "zerodisk": true, "randomdisk": true,
	"partitiondisk": true, "repartitiondisk": true, "reformat": true, "mergepartitions": true,
	"splitpartition": true, "eraseblock": true,
}

// partitioners edit a partition table; a listing option makes them read-only.
var partitioners = map[string]bool{
	"fdisk": true, "sfdisk": true, "gdisk": true, "sgdisk": true, "cfdisk": true, "parted": true, "cgdisk": true,
}

// mkfsLike creates a filesystem or swap area on its target.
func mkfsLike(name string) bool {
	switch name {
	case "mkfs", "mke2fs", "mkswap", "mkdosfs", "mkntfs", "newfs", "mkudffs", "mkreiserfs":
		return true
	}
	return strings.HasPrefix(name, "mkfs.") || strings.HasPrefix(name, "newfs_")
}

// catastrophic judges the effective command of the segment, no runner payloads.
func (fs *floorSeg) catastrophic() bool {
	name := fs.name()
	args := fs.argv[1:]
	if (name == "busybox" || name == "toybox") && len(args) > 0 {
		sub := *fs
		sub.argv = args
		return sub.catastrophic()
	}
	if containsWord(args, "--no-preserve-root") {
		return true // rm's own override of its safeguard, whatever the binary is called
	}
	lname := strings.ToLower(name)
	switch {
	case powerCmds[name]:
		// Judged only as the real command: after a runner (`ssh host reboot`) the word
		// may be a host name or an argument.
		return !fs.viaSuffix && !containsWord(args, "--help") && !containsWord(args, "--version") && !containsWord(args, "-c") && !containsWord(args, "--show")
	case windowsDiskCmds[lname]:
		return true
	case mkfsLike(name):
		return fs.anyDevice(args)
	}
	switch name {
	case "rm", "remove-item", "ri", "del", "erase", "rd", "rmdir":
		return recursiveFlag(args, fs.pl.windows) && fs.anyRootLike(args)
	case "shred", "blkdiscard":
		return fs.anyDevice(args) || (name == "shred" && fs.anyRootLike(args))
	case "chmod", "chown", "chgrp":
		return recursiveFlag(args, false) && fs.anyRootLike(args)
	case "mv":
		w := nonFlags(args)
		return len(w) > 1 && fs.anyRootLike(w[:len(w)-1])
	case "find":
		return fs.findCatastrophic(args)
	case "dd":
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok && fs.isDevice(v) {
				return true
			}
		}
	case "wipefs":
		for _, a := range args {
			if a == "--all" || a == "--force" || shortCluster(a, "afo") {
				return fs.anyDevice(args)
			}
		}
	case "badblocks":
		return containsWord(args, "-w") && fs.anyDevice(args)
	case "hdparm":
		for _, a := range args {
			if strings.Contains(a, "security-erase") || strings.Contains(a, "security-set-pass") {
				return true
			}
		}
	case "nvme":
		w := nonFlags(args)
		return len(w) > 0 && (w[0] == "format" || w[0] == "sanitize") && fs.anyDevice(args)
	case "cryptsetup":
		for _, a := range args {
			switch a {
			case "luksFormat", "luksErase", "erase", "reencrypt":
				return true
			}
		}
	case "diskutil":
		w := nonFlags(args)
		if len(w) > 0 && diskutilDestroys[strings.ToLower(w[0])] {
			return true
		}
		if len(w) > 1 && strings.EqualFold(w[0], "apfs") && strings.HasPrefix(strings.ToLower(w[1]), "delete") {
			return true
		}
	case "systemctl", "loginctl":
		w := nonFlags(args)
		return !fs.viaSuffix && !containsWord(args, "--help") && len(w) > 0 && (w[0] == "poweroff" || w[0] == "reboot" || w[0] == "halt" || w[0] == "kexec")
	case "init", "telinit":
		w := nonFlags(args)
		return !fs.viaSuffix && len(w) > 0 && (w[0] == "0" || w[0] == "6")
	case "format":
		if fs.pl.windows {
			for _, a := range args {
				if len(a) >= 2 && isLetter(a[0]) && a[1] == ':' {
					return true
				}
			}
		}
	case "cipher":
		for _, a := range args {
			if strings.HasPrefix(strings.ToLower(a), "/w") {
				return true
			}
		}
	}
	if partitioners[name] {
		for _, a := range args {
			switch a {
			case "-l", "--list", "-p", "--print", "print", "p", "--show", "-V", "--version", "-h", "--help":
				return false
			}
		}
		return fs.anyDevice(args)
	}
	return false
}

// findCatastrophic is `find <root-like> ... -delete` or -exec of a remover.
func (fs *floorSeg) findCatastrophic(args []string) bool {
	var roots []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
			break
		}
		roots = append(roots, a)
	}
	if !fs.anyRootLike(roots) {
		return false
	}
	for i, a := range args {
		switch a {
		case "-delete":
			return true
		case "-exec", "-execdir", "-ok", "-okdir":
			if i+1 < len(args) {
				switch fs.pl.cmdName(args[i+1]) {
				case "rm", "shred", "unlink", "mv", "chmod", "chown", "chgrp", "dd":
					return true
				}
			}
		}
	}
	return false
}

// anyRootLike reports a word (not an option) that names a catastrophic
// directory to act on.
func (fs *floorSeg) anyRootLike(args []string) bool {
	for _, w := range nonFlags(args) {
		if fs.rootLikeWord(w) {
			return true
		}
	}
	return false
}

// anyDevice reports a word, or an option's value, that names a block device.
func (fs *floorSeg) anyDevice(args []string) bool {
	for _, a := range args {
		if _, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") {
			a = v
		}
		if fs.isDevice(a) {
			return true
		}
	}
	return false
}

// rootLikeWord reports whether a word resolves to the filesystem root, the
// home, its parent or a top-level system directory. A trailing `/`, `/*` or
// `/.*` is dropped and a bare `*` or `.*` means the segment's cwd; any other
// glob stays dynamic (the engine asks).
func (fs *floorSeg) rootLikeWord(w string) bool {
	sep := fs.pl.sep()
	switch w {
	case "":
		return false
	case "*", ".*":
		w = "."
	}
	for _, tail := range []string{"/*", "/.*", `\*`, `\.*`} {
		w = strings.TrimSuffix(w, tail)
		if w == "" {
			w = sep
		}
	}
	if len(w) > 1 && strings.HasSuffix(w, "/") {
		if w = strings.TrimRight(w, "/"); w == "" {
			w = sep
		}
	}
	if fs.pl.windows && len(w) > 1 && strings.HasSuffix(w, "*") && !strings.ContainsAny(w[:len(w)-1], "*?[") {
		// The splitter reads `C:\*` as `C:*` (a backslash escapes the star).
		if w = strings.TrimSuffix(w, "*"); strings.HasSuffix(w, ":") {
			w += sep
		}
	}
	if strings.ContainsAny(w, "*?[") {
		return false
	}
	resolved, ok := resolveFloor(fs.req, w, fs.cwd)
	return ok && isRootLike(fs.req, resolved)
}

// posixSystemRoots are the top-level directories whose removal is never routine.
var posixSystemRoots = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/opt", "/proc", "/root", "/sbin", "/srv", "/sys", "/usr", "/var",
	"/home", "/Users", "/System", "/Library", "/Applications", "/Volumes", "/private",
}

// isRootLike is the catastrophic-target test for a resolved path.
func isRootLike(req Request, resolved string) bool {
	pl := req.pl()
	if pl.eq(pl.dir(resolved), resolved) {
		return true // a filesystem or volume root
	}
	if home := floorHome(req); home != "" {
		if rh, err := pl.resolve(home, ""); err == nil && (pl.eq(rh, resolved) || pl.eq(pl.dir(rh), resolved)) {
			return true
		}
	}
	var roots []string
	if pl.windows {
		vol := pl.volume(resolved)
		roots = append(roots, vol+`\Users`, vol+`\Windows`, vol+`\Program Files`, vol+`\Program Files (x86)`, vol+`\ProgramData`,
			envOr(pl, `C:\Windows`, "SystemRoot", "windir"),
			envOr(pl, `C:\Program Files`, "ProgramFiles"),
			envOr(pl, `C:\Program Files (x86)`, "ProgramFiles(x86)"),
			envOr(pl, `C:\ProgramData`, "ProgramData"))
	} else {
		roots = posixSystemRoots
	}
	for _, r := range roots {
		if rr, err := pl.resolve(r, ""); err == nil && pl.eq(rr, resolved) {
			return true
		}
	}
	return false
}

// posixBlockDevice names the block devices and raw memory a write must never reach.
var posixBlockDevice = regexp.MustCompile(`^/dev/(sd[a-z]+\d*|hd[a-z]+\d*|vd[a-z]+\d*|xvd[a-z]+\d*|nvme\d+(n\d+(p\d+)?)?|mmcblk\d+(p\d+)?|r?disk\d+(s\d+)*|loop\d+|md\d+|dm-\d+|mapper/.+|sr\d+|nbd\d+|zram\d+|ram\d+|mtdblock\d+|mem|kmem|port)$`)

// winBlockDevice names raw Windows disks and volumes (`\\.\PhysicalDrive0`); the splitter may eat one backslash.
var winBlockDevice = regexp.MustCompile(`(?i)^[\\/]{1,2}[.?][\\/](physicaldrive\d+|harddisk\d+|[a-z]:|volume\{.*)$`)

// isDevice reports a word that names a block device, directly or through a
// link (/dev/disk/by-id/... resolves to /dev/sdX).
func (fs *floorSeg) isDevice(w string) bool {
	if w == "" {
		return false
	}
	if winBlockDevice.MatchString(w) || posixBlockDevice.MatchString(strings.ReplaceAll(w, "//", "/")) {
		return true
	}
	if fs.pl.windows || !strings.HasPrefix(w, "/dev/") {
		return false
	}
	r, ok := resolveFloor(fs.req, w, fs.cwd)
	return ok && posixBlockDevice.MatchString(r)
}

// forkBombDef finds a shell function definition `name(){ body }`.
var forkBombDef = regexp.MustCompile(`([^\s;&|(){}'"]+)\s*\(\s*\)\s*\{([^}]*)\}`)

// forkWhileFork is perl's `fork while fork`.
var forkWhileFork = regexp.MustCompile(`\bfork\s+while\s+fork\b`)

// forkBomb reports a shell function that pipes itself into itself in the
// background (`:(){ :|:& };:`, `bomb(){ bomb|bomb& };bomb`) or perl's
// `fork while fork`, in a command that also runs something other than a
// reader or a text writer: the raw text is read because the splitter breaks
// the definition into pieces, and a search, a commit message or a
// `cat > f <<EOF` that merely names one is no bomb.
func forkBomb(segs []*floorSeg, raw string) bool {
	hit := forkWhileFork.MatchString(raw)
	for _, m := range forkBombDef.FindAllStringSubmatch(raw, -1) {
		n := regexp.QuoteMeta(m[1])
		if regexp.MustCompile(n + `\s*\|&?\s*` + n + `[^&]*&`).MatchString(m[2]) {
			hit = true
		}
	}
	if !hit {
		return false
	}
	// Only a command that can run the text counts: a shell, an interpreter, a
	// runner, or the function definition itself (its word looks like `name(){`).
	for _, fs := range segs {
		if len(fs.argv) == 0 {
			continue
		}
		name := fs.name()
		if shellNames[name] || commandRunners[name] || transportNames[name] || strings.HasPrefix(name, "python") ||
			strings.Contains(fs.argv[0], "()") || name == "source" || name == "." {
			return true
		}
	}
	return false
}
