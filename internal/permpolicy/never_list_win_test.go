package permpolicy

import "testing"

func winNeverHit(t *testing.T, cmd string) (FloorHit, bool) {
	t.Helper()
	_, req := winEnv(t)
	req.Command = cmd
	return CheckFloor(req, splitCommand(cmd, true))
}

func TestNeverList_WindowsDenied(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		`Remove-Item -Recurse -Force C:\`, `Remove-Item -Recurse C:\Users`, `Remove-Item -Recurse -Force C:\Users\bob\*`,
		`Remove-Item -Recurse $HOME`, `ri -r -fo C:\Windows`, `rd /s /q C:\`, `rmdir /s /q "C:\Program Files"`, `del /s /q C:\*`,
		`rm -rf C:\`, `rm -rf /c/Users`, `Format-Volume -DriveLetter D`, `Clear-Disk -Number 1 -RemoveData`, `diskpart /s wipe.txt`,
		`format D: /q`, `format.com C:`, `cipher /w:C:\`, `Remove-Partition -DriveLetter D`,
		`powershell -Command "Remove-Item -Recurse -Force C:\"`, `cmd /c "rd /s /q C:\"`, `Stop-Computer`, `shutdown /s /t 0`,
		`dd if=x of=\\.\PhysicalDrive0`, `mkfs.ext4 \\.\PhysicalDrive1`,
	} {
		if h, ok := winNeverHit(t, cmd); !ok {
			t.Errorf("CheckFloor(%q) = no hit", cmd)
		} else if h.Reason == "" {
			t.Errorf("hit %q has no reason", h.ID)
		}
	}
}

func TestNeverList_WindowsNearMissesAllowed(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		`Remove-Item -Recurse -Force .\build`, `Remove-Item -Recurse node_modules`, `rd /s /q build`, `del /s /q .\obj\*`,
		`rmdir /s /q C:\Work\Wt\dist`, `rm -rf ./build`, `Get-Volume`, `Get-Disk`, `format-json x.json`, `gofmt -w main.go`,
		`dotnet format`, `prettier --write x.js`, `cipher /e x.txt`, `format`, `type x.txt`, `dir C:\`, `Get-ChildItem C:\`,
	} {
		if h, ok := winNeverHit(t, cmd); ok && h.ID == "no-catastrophic" {
			t.Errorf("CheckFloor(%q) = %+v; want no never-list hit", cmd, h)
		}
	}
}
