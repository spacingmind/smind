//go:build windows

package securefile

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// restrict replaces path's DACL with a protected one (no inherited
// entries) granting full control to the current user and SYSTEM only.
func restrict(path string) error {
	return restrictWith(path, windows.NO_INHERITANCE)
}

// restrictDir is restrict for a directory whose future children must stay
// private too: the two entries are inheritable, so a file the directory's
// owner (or a program it launches) creates inside gets "current user and
// SYSTEM" and nothing from the parent's own, possibly broader, ACL.
func restrictDir(path string) error {
	return restrictWith(path, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
}

func restrictWith(path string, inheritance uint32) error {
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("securefile: SYSTEM sid: %w", err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		grant(user, inheritance),
		grant(system, inheritance),
	}, nil)
	if err != nil {
		return fmt.Errorf("securefile: build acl: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		return fmt.Errorf("securefile: set acl on %s: %w", path, err)
	}
	return nil
}

func grant(sid *windows.SID, inheritance uint32) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

// isPrivate checks that the DACL is protected and every ACE grants access
// to the current user or SYSTEM only.
func isPrivate(path string) (bool, error) {
	return ownerOnly(path, true)
}

// isPrivateTree checks root the way isPrivate does, then every descendant:
// a child created inside a restrictDir directory inherits its DACL (so it is
// not "protected" itself), but it must grant the current user and SYSTEM
// only. A single file with a broader entry makes the tree not private.
func isPrivateTree(root string) (bool, error) {
	if ok, err := isPrivate(root); err != nil || !ok {
		return false, err
	}
	private := true
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		ok, err := ownerOnly(path, false)
		if err != nil {
			return err
		}
		if !ok {
			private = false
			return filepath.SkipAll
		}
		return nil
	})
	return private, err
}

// ownerOnly reports whether every allow entry of path's DACL names the
// current user or SYSTEM; requireProtected also demands the DACL does not
// inherit from its parent (the form restrict leaves).
func ownerOnly(path string, requireProtected bool) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, fmt.Errorf("securefile: read acl of %s: %w", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	if requireProtected && control&windows.SE_DACL_PROTECTED == 0 {
		return false, nil
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false, err
	}
	user, err := currentUserSID()
	if err != nil {
		return false, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false, err
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue // deny entries can only narrow access
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user) && !sid.Equals(system) {
			return false, nil
		}
	}
	return true, nil
}

func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	u, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("securefile: current user: %w", err)
	}
	return u.User.Sid, nil
}
