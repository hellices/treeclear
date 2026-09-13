package fssecure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentUserFixture(test *testing.T) *windows.SID {
	test.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		test.Fatal(err)
	}
	return user.User.Sid
}

func setDescriptorFixture(test *testing.T, path, sddl string, protected bool) {
	test.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		test.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		test.Fatal(err)
	}
	information := windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	if protected {
		information = windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.SECURITY_INFORMATION(information), currentUserFixture(test), nil, dacl, nil); err != nil {
		test.Fatal(err)
	}
}

func makePrivateFixture(test *testing.T, path string, directory bool) {
	test.Helper()
	setDescriptorFixture(test, path, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)", currentUserFixture(test)), true)
}

func makeBroadFixture(test *testing.T, path string, directory bool) {
	test.Helper()
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	setDescriptorFixture(test, path, fmt.Sprintf("D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;WD)", inheritance, currentUserFixture(test), inheritance, inheritance), true)
}

func securitySnapshot(test *testing.T, path string) string {
	test.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		test.Fatal(err)
	}
	return descriptor.String()
}

func assertPrivateObject(test *testing.T, path string, directory bool) {
	test.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		test.Fatal(err)
	}
	if info.IsDir() != directory || !directory && !info.Mode().IsRegular() {
		test.Fatalf("unexpected file type for %q: %v", path, info.Mode())
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		test.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(currentUserFixture(test)) {
		test.Fatalf("private object owner = %v, %v; want current user", owner, err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 || control&windows.SE_DACL_PRESENT == 0 {
		test.Fatalf("DACL is not present and protected: control=%#x, error=%v", control, err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		test.Fatalf("expected exactly two private ACEs: %v, %v", dacl, err)
	}
	trustees := make(map[string]bool)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &entry); err != nil {
			test.Fatal(err)
		}
		if entry.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || entry.Header.AceFlags != 0 || entry.Mask != windows.STANDARD_RIGHTS_REQUIRED|windows.SYNCHRONIZE|0x1ff {
			test.Fatalf("unexpected ACE permissions/type/inheritance: %#v", entry)
		}
		trustee := (*windows.SID)(unsafe.Pointer(&entry.SidStart)).String()
		if trustee != currentUserFixture(test).String() && trustee != "S-1-5-18" {
			test.Fatalf("unexpected allowed trustee %q", trustee)
		}
		trustees[trustee] = true
	}
	if !trustees[currentUserFixture(test).String()] || !trustees["S-1-5-18"] {
		test.Fatalf("missing current user or SYSTEM trustee: %v", trustees)
	}
}

func symlinkCapabilityUnavailable(err error) bool {
	return errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_NOT_SUPPORTED)
}

func TestReadPrivateFileWindowsRejectsUnprotectedDACL(test *testing.T) {
	parent := privateDirectoryFixture(test)
	path := filepath.Join(parent, "unprotected")
	writePrivateFixture(test, path, []byte("private"))
	setDescriptorFixture(test, path, fmt.Sprintf("D:(A;;FA;;;%s)(A;;FA;;;SY)", currentUserFixture(test)), false)
	before := securitySnapshot(test, path)
	if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
		test.Fatalf("unprotected DACL ReadPrivateFile = %q, %v; want nil and error", contents, err)
	}
	if after := securitySnapshot(test, path); after != before {
		test.Fatal("read changed unprotected DACL")
	}
}

func TestPrivateOperationsWindowsRejectAlternateStreamsAndDevices(test *testing.T) {
	parent := privateDirectoryFixture(test)
	base := filepath.Join(parent, "base")
	writePrivateFixture(test, base, []byte("unchanged"))
	for _, path := range []string{base + ":stream", filepath.Join(parent, "NUL"), filepath.Join(parent, "CON"), `\\.\NUL`} {
		if err := EnsurePrivateDirectory(path); err == nil {
			test.Errorf("EnsurePrivateDirectory accepted %q", path)
		}
		if err := WritePrivateFile(path, []byte("sensitive")); err == nil {
			test.Errorf("WritePrivateFile accepted %q", path)
		}
		if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
			test.Errorf("ReadPrivateFile accepted %q", path)
		}
	}
	assertContents(test, base, []byte("unchanged"))
	assertOnlyNames(test, parent, "base")
}

func TestWindowsSecurityDescriptorValidation(test *testing.T) {
	owner := currentUserFixture(test).String()
	for _, entry := range []struct {
		name    string
		sddl    string
		allowed bool
	}{
		{name: "owner-and-system", sddl: "O:%sD:P(A;;FA;;;%s)(A;;FA;;;SY)", allowed: true},
		{name: "owner-only", sddl: "O:%sD:P(A;;FA;;;%s)", allowed: true},
		{name: "missing-owner", sddl: "D:P(A;;FA;;;%s)"},
		{name: "foreign-owner", sddl: "O:WDD:P(A;;FA;;;%s)"},
		{name: "unprotected", sddl: "O:%sD:(A;;FA;;;%s)(A;;FA;;;SY)"},
		{name: "empty-dacl", sddl: "O:%sD:P"},
		{name: "everyone", sddl: "O:%sD:P(A;;FA;;;%s)(A;;FA;;;WD)"},
		{name: "administrators", sddl: "O:%sD:P(A;;FA;;;%s)(A;;FA;;;BA)"},
		{name: "inherited-ace", sddl: "O:%sD:P(A;ID;FA;;;%s)(A;;FA;;;SY)"},
		{name: "inherit-only-ace", sddl: "O:%sD:P(A;IO;FA;;;%s)(A;;FA;;;SY)"},
		{name: "inheritable-ace", sddl: "O:%sD:P(A;OICI;FA;;;%s)(A;;FA;;;SY)"},
		{name: "partial-owner-access", sddl: "O:%sD:P(A;;FR;;;%s)(A;;FA;;;SY)"},
		{name: "deny-ace", sddl: "O:%sD:P(D;;FA;;;%s)(A;;FA;;;SY)"},
	} {
		test.Run(entry.name, func(test *testing.T) {
			descriptor, err := windows.SecurityDescriptorFromString(strings.ReplaceAll(entry.sddl, "%s", owner))
			if err != nil {
				test.Fatalf("construct security fixture: %v", err)
			}
			if err := verifyWindowsDescriptor(descriptor); (err == nil) != entry.allowed {
				test.Fatalf("verifyWindowsDescriptor = %v; allowed=%v", err, entry.allowed)
			}
		})
	}
	for _, present := range []bool{false, true} {
		test.Run(fmt.Sprintf("null-dacl-present-%t", present), func(test *testing.T) {
			descriptor, err := windows.NewSecurityDescriptor()
			if err != nil {
				test.Fatal(err)
			}
			if err := descriptor.SetOwner(currentUserFixture(test), false); err != nil {
				test.Fatal(err)
			}
			if err := descriptor.SetDACL(nil, present, false); err != nil {
				test.Fatal(err)
			}
			if err := descriptor.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
				test.Fatal(err)
			}
			if err := verifyWindowsDescriptor(descriptor); err == nil {
				test.Fatal("accepted a null or absent DACL")
			}
		})
	}
}

func TestCreatePrivateFileWindowsACLBeforeWriting(test *testing.T) {
	parent := test.TempDir()
	makeBroadFixture(test, parent, true)
	before := securitySnapshot(test, parent)
	path := filepath.Join(parent, "staging")
	file, err := createPrivateFile(path)
	if err != nil {
		test.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != 0 {
		test.Fatalf("staging file is not empty: %v, %v", info, err)
	}
	assertPrivateObject(test, path, false)
	if after := securitySnapshot(test, parent); after != before {
		test.Fatal("staging creation changed the broad ancestor ACL")
	}
}

func TestReadPrivateFileWindowsAllowsOwnerOnlyDACL(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "owner-only")
	writePrivateFixture(test, path, []byte("owner only"))
	setDescriptorFixture(test, path, fmt.Sprintf("D:P(A;;FA;;;%s)", currentUserFixture(test)), true)
	contents, err := ReadPrivateFile(path, 32)
	if err != nil || string(contents) != "owner only" {
		test.Fatalf("ReadPrivateFile = %q, %v", contents, err)
	}
}

func TestPrivateFilesWindowsLongPaths(test *testing.T) {
	parent := privateDirectoryFixture(test)
	for range 20 {
		parent = filepath.Join(parent, "long-path-component")
	}
	path := filepath.Join(parent, "plan.json")
	if err := WritePrivateFile(path, []byte("long path")); err != nil {
		test.Fatal(err)
	}
	contents, err := ReadPrivateFile(path, 32)
	if err != nil || string(contents) != "long path" {
		test.Fatalf("ReadPrivateFile = %q, %v", contents, err)
	}
	assertPrivateObject(test, path, false)
}
