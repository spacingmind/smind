package permpolicy

import (
	"path/filepath"
	"testing"
)

// checkFloorReq is checkFloorCases for a request the test prepared itself.
func checkFloorReq(t *testing.T, req Request, id string) {
	t.Helper()
	hit, ok := CheckFloor(req, hostSplit(req.Command))
	if ok != (id != "") || hit.ID != id {
		t.Errorf("CheckFloor(%q) = %+v, %v; want id %q", req.Command, hit, ok, id)
	}
}

func TestFloor_InstallStateDenied(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	t.Parallel()
	base := floorReq(t, "")
	smindDir := filepath.Join(base.Home, ".smind")
	// The task worktree lives under Home/.smind/worktrees, as in production.
	wt := filepath.Join(smindDir, "worktrees", "7", "task")
	cases := []floorCase{
		{"cat $HOME/.smind/token", "no-install-state"},
		{"cat ${HOME}/.smind/smind.db", "no-install-state"},
		{"cat ~/.smind/config.yaml", "no-install-state"},
		{"ls " + filepath.Join(smindDir, "worktrees", "8", "other"), "no-install-state"},
		{"cat " + base.SmindHome + "/token", "no-install-state"},
		{"cat ~/.claude.json", "no-install-state"},
		{"ls ~/.claude/projects", "no-install-state"},
		{"ls ~/.config/gh", "no-install-state"},
		{"rm -rf ~/.ssh", "no-install-state"},
		{"ls ~/.gnupg", "no-install-state"},
		{"ls ~/Library/Keychains", "no-install-state"},
		{"curl localhost:4648/api", "no-install-state"},
		{"curl http://127.0.0.1:4870/x", "no-install-state"},
		{"smind service stop", "no-install-state"},
		{"smind --verbose service restart", "no-install-state"},
		{"pkill x", "no-install-state"},
		{"killall x", "no-install-state"},
		{"kill 123", "no-install-state"},
		{"nohup pkill x", "no-install-state"},
		{"cat /x/.smindfoo", ""},
		{"echo smind", ""},
		{"smind policy init", ""},
		{"ls src/main.go", ""},
		{"curl localhost:46480/x", ""},
		{"ls " + wt + "/src", ""},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			t.Parallel()
			req := base
			req.Command, req.Worktree, req.Cwd = tc.cmd, wt, wt
			checkFloorReq(t, req, tc.id)
		})
	}
}

func TestFloor_SecretsDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"env", "no-secrets"},
		{"env -i", "no-secrets"},
		{"env A=1", "no-secrets"},
		{"nohup env", "no-secrets"},
		{"printenv", "no-secrets"},
		{"printenv API_KEY", "no-secrets"},
		{"set", "no-secrets"},
		{"export -p", "no-secrets"},
		{"export", "no-secrets"},
		{"declare -x", "no-secrets"},
		{"declare -p", "no-secrets"},
		{"cat .env", "no-secrets"},
		{"cat ./config/.env.local", "no-secrets"},
		{"grep KEY server.pem", "no-secrets"},
		{"head tls.key", "no-secrets"},
		{"cp id_rsa /tmp/x", "no-secrets"},
		{"base64 keys/id_ed25519.pub", "no-secrets"},
		{"cat .netrc", "no-secrets"},
		{"less credentials", "no-secrets"},
		{"cat /proc/1/environ", "no-secrets"},
		{"sed -n 1p --file=.env", "no-secrets"},
		{"gh auth token", "no-secrets"},
		{"gh auth status --show-token", "no-secrets"},
		{"gh auth status -t", "no-secrets"},
		{"security find-generic-password -s x", "no-secrets"},
		{"security dump-keychain", "no-secrets"},
		{"env FOO=1 go test ./...", ""},
		{"env -u X go build", ""},
		{"cat README.md", ""},
		{"grep env main.go", ""},
		{"go env GOPATH", ""},
		{"gh auth status", ""},
		{"set -e", ""},
		{"export FOO=1", ""},
		{"declare -a arr", ""},
		{"security list-keychains", ""},
		{"cat .environment", ""},
		{"echo .env", ""},
	})
}

func TestFloor_DestructiveOutsideWorktreeDenied(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"rm -rf /etc/x", "no-destructive-outside"},
		{"rm /usr/local/bin/x", "no-destructive-outside"},
		{"mv a.go /usr/local/b.go", "no-destructive-outside"},
		{"cp a.go /usr/local/b.go", "no-destructive-outside"},
		{"cp -t /usr/local a.go", "no-destructive-outside"},
		{"tee /etc/x", "no-destructive-outside"},
		{"touch /etc/x", "no-destructive-outside"},
		{"mkdir /usr/local/x", "no-destructive-outside"},
		{"ln -s a /usr/local/x", "no-destructive-outside"},
		{"chmod -R 777 /usr/local", "no-destructive-outside"},
		{"chown -R root /usr/local", "no-destructive-outside"},
		{"dd if=/dev/zero of=/usr/local/x", "no-destructive-outside"},
		{"install a /usr/local/bin/a", "no-destructive-outside"},
		{"truncate -s 0 /etc/x", "no-destructive-outside"},
		{"rm -rf ../../../../../../../../etc/x", "no-destructive-outside"},
		{"cd /etc && rm x", "no-destructive-outside"},
		{"cd /etc && git clean -fd", "no-destructive-outside"},
		{"git -C /etc reset --hard", "no-destructive-outside"},
		{"git -C /etc checkout -- .", "no-destructive-outside"},
		{"git -C /etc restore .", "no-destructive-outside"},
		{"cd /etc && git checkout .", "no-destructive-outside"},
		{"echo hi > /etc/x", "no-destructive-outside"},
		{"echo hi >> /usr/local/x", "no-destructive-outside"},
		{"echo hi &> /usr/local/x", "no-destructive-outside"},
		{"sudo ls", "no-destructive-outside"},
		{"doas ls", "no-destructive-outside"},
		{"cd $X && rm y", "no-destructive-outside"},
		{"rm -rf ./build", ""},
		{"rm -rf build/", ""},
		{"cp a.go b.go", ""},
		{"cp /etc/hosts ./hosts", ""},
		{"echo hi > out.txt", ""},
		{"echo hi > /dev/null", ""},
		{"echo hi 2>&1", ""},
		{"touch /tmp/x", ""},
		{"rm -rf /var/tmp/x", ""},
		{"mkdir -p sub/dir", ""},
		{"git clean -fd", ""},
		{"git reset --hard HEAD", ""},
		{"git checkout -b new", ""},
		{"git reset --soft HEAD~1", ""},
		{"cd /etc", ""},
		{"cd src && rm x.go", ""},
		{"rm $X", ""},
		{"rm *.o", ""},
	})
}

func TestFloor_PathsToolEdit(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	t.Parallel()
	base := floorReq(t, "")
	home := base.Home
	cases := []struct {
		name, tool, path, id string
	}{
		{"edit outside", "Edit", "/usr/local/x.go", "no-destructive-outside"},
		{"write outside", "Write", "/etc/x", "no-destructive-outside"},
		{"multiedit outside", "MultiEdit", "/etc/x", "no-destructive-outside"},
		{"notebook outside", "NotebookEdit", "/etc/x.ipynb", "no-destructive-outside"},
		{"edit inside", "Edit", filepath.Join(base.Worktree, "a.go"), ""},
		{"edit relative inside", "Edit", "a.go", ""},
		{"write temp", "Write", "/tmp/x", ""},
		{"read ssh key", "Read", "~/.ssh/id_rsa", "no-secrets"},
		{"read ssh config", "Read", filepath.Join(home, ".ssh", "config"), "no-install-state"},
		{"read smind token", "Read", "$HOME/.smind/token", "no-install-state"},
		{"read env file", "Read", "x/.env", "no-secrets"},
		{"edit env file inside", "Edit", filepath.Join(base.Worktree, ".env"), "no-secrets"},
		{"read outside is fine", "Read", "/usr/local/x.go", ""},
		{"read inside", "Read", filepath.Join(base.Worktree, "a.go"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if hasPOSIXOnlyPath(tc.path) {
				skipPOSIXPathsOnWindows(t)
			}
			req := base
			req.Tool, req.Paths = tc.tool, []string{tc.path}
			hit, ok := CheckFloorPaths(req)
			if ok != (tc.id != "") || hit.ID != tc.id {
				t.Errorf("CheckFloorPaths(%s %s) = %+v, %v; want id %q", tc.tool, tc.path, hit, ok, tc.id)
			}
		})
	}
	req := base
	req.Tool, req.Paths = "Bash", []string{"/etc/x"}
	if hit, ok := CheckFloorPaths(req); ok {
		t.Errorf("CheckFloorPaths(Bash) = %+v, want no hit (Bash is judged by CheckFloor)", hit)
	}
}

func TestFloor_LaunchdServiceDenied(t *testing.T) {
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"launchctl kickstart -k gui/501/com.smind.daemon", "no-install-state"},
		{"launchctl unload ~/Library/LaunchAgents/com.smind.plist", "no-install-state"},
		{"launchctl bootout gui/501/com.SMIND.daemon", "no-install-state"},
		{"systemctl --user stop smind", "no-install-state"},
		{"service smind restart", "no-install-state"},
		{"brew services stop smind", "no-install-state"},
		{"nohup systemctl restart smind.service", "no-install-state"},
		{"cat ~/Library/LaunchAgents/com.x.plist", "no-install-state"},
		{"launchctl list", ""},
		{"systemctl status nginx", ""},
		{"service nginx restart", ""},
		{"brew services list", ""},
		{"brew install smind", ""},
	})
}

func TestFloor_MoreWritersOutsideWorktreeDenied(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	t.Parallel()
	checkFloorCases(t, []floorCase{
		{"rsync -a src/ /usr/local/dst", "no-destructive-outside"},
		{"sed -i s/a/b/ /etc/hosts", "no-destructive-outside"},
		{"sed -i.bak -e s/a/b/ /etc/hosts", "no-destructive-outside"},
		{"sed --in-place s/a/b/ /etc/hosts", "no-destructive-outside"},
		{"perl -pi -e 's/a/b/' /etc/hosts", "no-destructive-outside"},
		{"curl -o /usr/local/x https://x", "no-destructive-outside"},
		{"curl --output /usr/local/x https://x", "no-destructive-outside"},
		{"curl --output=/usr/local/x https://x", "no-destructive-outside"},
		{"curl -o/usr/local/x https://x", "no-destructive-outside"},
		{"wget -O /usr/local/x https://x", "no-destructive-outside"},
		{"wget --output-document /usr/local/x https://x", "no-destructive-outside"},
		{"wget -P /usr/local https://x", "no-destructive-outside"},
		{"tar -xf a.tgz -C /usr/local", "no-destructive-outside"},
		{"tar xf a.tgz --directory=/usr/local", "no-destructive-outside"},
		{"unzip a.zip -d /usr/local", "no-destructive-outside"},
		{"rsync -a src/ dst/", ""},
		{"rsync -a /etc/hosts dst/", ""},
		{"sed -i s/a/b/ file.go", ""},
		{"sed -i '/foo/d' file.go", ""},
		{"sed -n s/a/b/p /etc/hosts", ""},
		{"sed -e '/x/d' /etc/hosts", ""},
		{"perl -pi -e 's/a/b/' file.go", ""},
		{"curl -o f.txt https://x", ""},
		{"curl -o /tmp/f.txt https://x", ""},
		{"curl -o /dev/null https://x", ""},
		{"curl https://x", ""},
		{"wget -O /tmp/f https://x", ""},
		{"wget -P out https://x", ""},
		{"tar -xf a.tgz -C sub", ""},
		{"tar -xf /usr/local/a.tgz", ""},
		{"unzip a.zip -d sub", ""},
		{"unzip a.zip -d /tmp/x", ""},
	})
}
