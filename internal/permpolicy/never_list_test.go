package permpolicy

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// neverReq is a POSIX request whose home is a fixed path outside the OS temp
// zone (a temp home would make `rm -rf ~` look like scratch cleanup).
func neverReq(t *testing.T, cmd string) Request {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	wt := t.TempDir()
	return Request{Tool: "Bash", Command: cmd, Home: "/home/dev", SmindHome: "/home/dev/.smind", Worktree: wt, Cwd: wt}
}

func neverHit(t *testing.T, cmd string) (FloorHit, bool) {
	t.Helper()
	return CheckFloor(neverReq(t, cmd), hostSplit(cmd))
}

func TestNeverList_Denied(t *testing.T) {
	t.Parallel()
	cmds := []string{
		// rm of a root-like target, every spelling of it
		"rm -rf ~", "rm -rf ~/", "rm -rf ~/*", "rm -rf $HOME", "rm -rf ${HOME}/", "rm -rf $HOME/*",
		"rm -rf /*", "rm -fr /", "rm -r -f -- /", "rm -rf '/'", `rm -rf "/"`, "rm -rf /home", "rm -rf /home/",
		"rm -rf /etc", "rm -rf /usr/", "rm -rf /Users", "rm -rf /System", "rm -Rf /",
		"rm -rf --no-preserve-root /", "mything --no-preserve-root /",
		"cd ~ && rm -rf *", "cd / && rm -rf .", "cd /home/dev/src && rm -rf ..",
		// moves, tree-wide mode changes, find
		"mv ~ /dev/null", "mv /etc /dev/null", "chmod -R 777 /", "chown -R me ~", "chgrp -R x /etc",
		"find / -delete", "find ~ -delete", "find / -name x -delete", "find ~ -exec rm -rf {} +", "find / -exec shred {} ;",
		// devices
		"mkfs.ext4 /dev/sda1", "mkfs -t ext4 /dev/sda", "mkfs.vfat -F32 /dev/disk2s1", "mke2fs /dev/nvme0n1p2",
		"mkswap /dev/sdb2", "newfs_hfs /dev/disk4", "wipefs -a /dev/sda", "wipefs --all /dev/sda", "blkdiscard /dev/sda",
		"shred -n3 /dev/sda", "shred -z /dev/disk/../sda", "dd if=/dev/zero of=/dev/sda", "dd if=x of=/dev/nvme0n1 bs=1M",
		"cat x > /dev/sda", "echo hi > /dev/disk2", ": > /dev/nvme0n1", "> /dev/sda",
		"sgdisk --zap-all /dev/sda", "parted -s /dev/sda mklabel gpt", "fdisk /dev/sda", "sfdisk /dev/sda",
		"cryptsetup luksFormat /dev/sda2", "cryptsetup erase /dev/sda2", "nvme format /dev/nvme0n1", "hdparm --security-erase NULL /dev/sda",
		"diskutil eraseDisk JHFS+ X disk2", "diskutil secureErase 0 disk2", "diskutil zeroDisk disk2", "diskutil apfs deleteContainer disk3",
		"diskutil partitionDisk disk2 GPT JHFS+ x 100%",
		// fork bombs
		":(){ :|:& };:", ":(){ :|: & };:", "bomb(){ bomb|bomb& };bomb", "perl -e 'fork while fork'",
		// power
		"shutdown -h now", "reboot", "poweroff", "halt", "systemctl poweroff", "systemctl reboot", "init 0",
		// wrappers
		"env A=1 rm -rf ~", "command rm -rf /", "nohup rm -rf /", "timeout 5 rm -rf /", "/bin/rm -rf /", "busybox rm -rf /",
		"xargs rm -rf /", "sh -c 'rm -rf /'", "bash -lc 'mkfs.ext4 /dev/sda'", "exec rm -rf ~",
		"su -c 'rm -rf /'", "su root -c 'rm -rf ~'", "ssh host 'rm -rf /'", "ssh host rm -rf /", "eval 'rm -rf /'",
		"eval 'mkfs.ext4 /dev/sda'", "echo 'rm -rf /' | sh", "echo rm -rf / | bash", "sh <<< 'rm -rf /'", "printf 'rm -rf ~\\n' | sh",
		"nsenter -t 1 -m rm -rf /", "setpriv --reuid 0 mkfs /dev/sda", "chroot /x rm -rf /", "runuser -u x -- rm -rf /",
		"parallel rm -rf ::: /", "flock /tmp/l rm -rf /", "script -c 'rm -rf /' /dev/null", "at now <<< 'rm -rf /'",
		"su -c 'su -c \"rm -rf /\"'", "find . -exec sh -c 'rm -rf /' ;", "bash -c ':(){ :|:& };:'", "ssh h ':(){ :|:& };:'",
		"echo ':(){ :|:& };:' | sh", "su -c 'diskutil eraseDisk JHFS+ X disk2'", "ssh h 'reboot'",
		"su -c 'mv ~ /dev/null'", "ssh h mv /usr /tmp/x", "su -c 'dd if=/dev/zero of=/dev/sda'", "su -c 'echo x > /dev/sda'", "ssh h 'cat x > /dev/disk2'",
		"sh <<EOF\nrm -rf /\nEOF", "fish -c 'rm -rf /'", "echo ok && sh <<'X'\nmkfs /dev/sda\nX",
	}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			h, ok := neverHit(t, cmd)
			if !ok {
				t.Fatalf("CheckFloor(%q) = no hit", cmd)
			}
			if h.Reason == "" {
				t.Fatalf("hit %q has no reason", h.ID)
			}
		})
	}
}

// Commands the old floor already denied keep their id: the never-list sits behind them.
func TestNeverList_ExistingIDsKept(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"rm -rf /", "dd if=/dev/zero of=/dev/sda", "cat x > /dev/sda", "chmod -R 777 /"} {
		h, ok := neverHit(t, cmd)
		if !ok || h.ID != "no-destructive-outside" {
			t.Errorf("CheckFloor(%q) = %+v, %v; want no-destructive-outside", cmd, h, ok)
		}
	}
}

func TestNeverList_NewIDForNewHits(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"mkfs.ext4 /dev/sda1", "rm -rf ~/*", ":(){ :|:& };:", "su -c 'rm -rf /'", "diskutil eraseDisk JHFS+ X disk2", "reboot"} {
		h, ok := neverHit(t, cmd)
		if !ok || h.ID != "no-catastrophic" {
			t.Errorf("CheckFloor(%q) = %+v, %v; want no-catastrophic", cmd, h, ok)
		}
	}
}

func TestNeverList_NearMissesAllowed(t *testing.T) {
	t.Parallel()
	cmds := []string{
		"rm -rf ./build", "rm -rf node_modules", "rm -rf build/ dist/", "rm -rf ./*", "rm -rf *", "rm -rf .git/hooks",
		"rm -rf ~/code/old-project", "rm -rf /tmp/x", "rm -rf /home/dev/proj/build", "rm -f /home/dev/x.txt", "rm /etc",
		"rm -rf /usr/local/share/foo/", "rm -rf $TMPDIR/x", "rmdir build",
		"chmod -R 755 ./scripts", "chown -R me ./data", "mv a b", "mv build /tmp/build", "find . -name '*.o' -delete",
		"find ./node_modules -type d -empty -delete", "find / -name foo", "find ~ -name '*.go' -print",
		"mkfs.ext4 disk.img", "mkfs.ext4 -F ./disk.img", "mke2fs -t ext4 /tmp/x.img", "mkswap ./swapfile",
		"dd if=/dev/zero of=./disk.img bs=1M count=1", "dd if=/dev/urandom of=/dev/null count=1", "dd if=/dev/sda of=./backup.img",
		"echo hi > /dev/null", "cat x > ./out", "cat /dev/zero | head -c 10 > ./z", "wipefs /dev/sda", "wipefs --help",
		"fdisk -l", "fdisk -l /dev/sda", "parted /dev/sda print", "parted -l", "sgdisk -p /dev/sda", "lsblk", "blkid /dev/sda1",
		"cryptsetup status x", "cryptsetup luksOpen /dev/sda2 x", "diskutil list", "diskutil info disk2", "diskutil unmount disk2",
		"diskutil apfs list", "shred secrets.txt", "shred -u ./old.log",
		"gofmt -w main.go", "go fmt ./...", "prettier --write foo.js", "dotnet format", "terraform fmt", "format-json x.json", "black .",
		"clang-format -i x.c", "format c", "formatter --write x",
		"echo ':(){ :|:& };:'", "grep -rn ':(){' docs", "rg 'fork while fork' .", "git commit -m 'document the :(){ :|:& };: fork bomb'",
		"cat > notes.md <<'EOF'\n:(){ :|:& };:\nEOF", "echo 'rm -rf /' > notes.txt", "echo 'rm -rf /'", "echo mkfs /dev/sda", "grep -rn 'rm -rf /' docs",
		"git commit -m 'never run rm -rf ~'", "echo 'rm -rf ./build' | sh", "sh -c 'rm -rf ./build'", "su -c 'ls /'", "ssh host 'ls /etc'", "eval 'echo hi'",
		"ssh host rm -rf ./build", "nsenter -t 1 ls", "ssh host systemctl status reboot", "ssh reboot", "ssh host -- ls halt", "systemctl list-units halt", "systemctl reboot --help x", "npm test -- 'fork while fork'", "go test -args 'a(){ a|a& }'", "sh -c 'echo reboot'", "echo reboot", "git log --grep=reboot", "go test -run TestReboot ./...",
		"systemctl status sshd", "init", "shutdown --help", "cd /tmp && rm -rf *", "cd /home/dev/proj && rm -rf *", "systemctl restart nginx",
		"rm -rf --preserve-root ./x", "hdparm -I /dev/sda", "nvme list", "badblocks /dev/sda",
	}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			if h, ok := neverHit(t, cmd); ok && h.ID == "no-catastrophic" {
				t.Errorf("CheckFloor(%q) = %+v; want no never-list hit", cmd, h)
			}
		})
	}
}

// The rm near misses that the old floor denied for a different reason (a
// target outside the worktree) are not the never-list's business: only the
// id is checked, so a future change cannot silently move them.
func TestNeverList_RootLikeTargets(t *testing.T) {
	t.Parallel()
	req := neverReq(t, "")
	fs := &floorSeg{req: req, cwd: "/home/dev/src", pl: req.pl()}
	for w, want := range map[string]bool{
		"/": true, "/*": true, "//": true, "~": true, "~/": true, "~/*": true, "$HOME": true, "${HOME}/*": true,
		"..": true, "../": true, "/home": true, "/home/": true, "/usr": true, "/etc/": true, "/Users": true,
		".": false, "*": false, "./*": false, "src": false, "~/src": false, "/home/dev/src": false, "/usr/local": false,
		"/tmp": false, "/etc/hosts": false, "$FOO": false, "~/*/x": false, "a*": false, "": false,
	} {
		if got := fs.rootLikeWord(w); got != want {
			t.Errorf("rootLikeWord(%q) = %v, want %v", w, got, want)
		}
	}
}

func TestNeverList_RootLikeWorktreeUnderHome(t *testing.T) {
	t.Parallel()
	// The worktree itself lives under the home; `rm -rf ~` must still be caught.
	req := Request{Tool: "Bash", Command: "rm -rf ~/*", Home: "/home/dev", SmindHome: "/home/dev/.smind",
		Worktree: "/home/dev/.smind/worktrees/1/wt", Cwd: "/home/dev/.smind/worktrees/1/wt"}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	if h, ok := CheckFloor(req, hostSplit(req.Command)); !ok || h.ID != "no-catastrophic" {
		t.Errorf("got %+v, %v", h, ok)
	}
}

func TestNeverList_ForkBombText(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		":(){ :|:& };:":                            true,
		"b(){ b | b & }; b":                        true,
		"x(){ x|&x& };x":                           true,
		"sh -c ':(){ :|:& };:'":                    true,
		"printf ':(){ :|:& };:' > x.sh && sh x.sh": true,
		"f(){ echo hi; }; f":                       false,
		"loop(){ loop; }; loop":                    false,
		"echo ':(){ :|:& };:'":                     false,
		"grep ':(){ :|:& };:' README":              false,
		"git commit -m ':(){ :|:& };:'":            false,
		"cat > x.md <<EOF\n:(){ :|:& };:\nEOF":     false,
		"f(){ g|h& }; f":                           false,
	} {
		p := hostSplit(cmd)
		req := neverReq(t, cmd)
		segs := make([]*floorSeg, len(p.Segments))
		for i, s := range p.Segments {
			segs[i] = newFloorSeg(req, s, req.Cwd)
		}
		if got := forkBomb(segs, cmd); got != want {
			t.Errorf("forkBomb(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestNeverList_RunnerTextRejudged(t *testing.T) {
	t.Parallel()
	for name := range commandRunners {
		if name == "find" || name == "watch" || name == "cron" || name == "crontab" || name == "open" || name == "osascript" || name == "xterm" || name == "tmux" || name == "screen" {
			continue // exercised through their own forms below or take no command as text
		}
		bad := name + " 'mkfs.ext4 /dev/sda'"
		if h, ok := neverHit(t, bad); !ok || h.ID == "" {
			t.Errorf("runner %s: %q not denied", name, bad)
		}
		good := name + " 'mkfs.ext4 disk.img'"
		if h, ok := neverHit(t, good); ok && h.ID == "no-catastrophic" {
			t.Errorf("runner %s: %q wrongly denied: %+v", name, good, h)
		}
	}
	for _, name := range []string{"xterm -e", "tmux new-session", "screen -dm", "open -a Terminal --args"} {
		cmd := name + " rm -rf /"
		if _, ok := neverHit(t, cmd); !ok {
			t.Errorf("%q not denied", cmd)
		}
	}
}

func TestDecide_NeverListCannotBeAllowedByPolicy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	pol := mustLoad(t, bashRule("allow-rm", ActionAllow, "rm", ""), bashRule("allow-all", ActionAllow, "mkfs.ext4", ""))
	for _, cmd := range []string{"rm -rf ~/*", "mkfs.ext4 /dev/sda1", "su -c 'rm -rf /'"} {
		req := neverReq(t, cmd)
		d := Decide(req, pol)
		if d.Action != ActionDeny || d.FloorID == "" {
			t.Errorf("Decide(%q) = %s floor=%q; want a floor deny", cmd, d.Action, d.FloorID)
		}
	}
}

func TestFloorReasons_AllNamed(t *testing.T) {
	t.Parallel()
	for id, reason := range floorReasons {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("floor id %q has no reason", id)
		}
	}
}

// Every floor id is named in the public page, the ADR table and the template.
func TestFloorReasons_InDocs(t *testing.T) {
	t.Parallel()
	for file, path := range map[string]string{
		"public page": "../../public-docs/permission-policy.md",
		"ADR-0047":    "../../docs/decisions/0047-steward-seat.md",
		"template":    "template.go",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for id := range floorReasons {
			if !strings.Contains(string(b), id) {
				t.Errorf("%s does not name floor id %q", file, id)
			}
		}
	}
}

// The parent of the home is catastrophic too, whatever the layout.
func TestNeverList_ParentOfHome(t *testing.T) {
	t.Parallel()
	req := neverReq(t, "")
	req.Home = "/data/people/dev"
	fs := &floorSeg{req: req, cwd: "/data/people/dev", pl: req.pl()}
	for w, want := range map[string]bool{"/data/people": true, "/data/people/dev": true, "..": true, "/data": false, "/data/people/other": false} {
		if got := fs.rootLikeWord(w); got != want {
			t.Errorf("rootLikeWord(%q) = %v, want %v", w, got, want)
		}
	}
}
