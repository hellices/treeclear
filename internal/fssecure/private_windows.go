package fssecure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileFullControl = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

func preparePrivatePath(path string) (string, error) {
	path = filepath.FromSlash(path)
	if strings.HasPrefix(strings.ToUpper(path), `\\?\UNC\`) {
		path = `\\` + path[8:]
	} else if strings.HasPrefix(path, `\\?\`) {
		path = path[4:]
		if len(path) < 3 || path[1] != ':' || path[2] != '\\' {
			return "", fmt.Errorf("unsupported Windows namespace: %w", fs.ErrInvalid)
		}
	}
	if strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\??\`) {
		return "", fmt.Errorf("unsupported Windows namespace: %w", fs.ErrInvalid)
	}
	if err := validateWindowsPrivatePath(path); err != nil {
		return "", err
	}
	path, err := resolvePrivateParents(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := validateWindowsPrivatePath(path); err != nil {
		return "", err
	}
	return path, nil
}

func validateWindowsPrivatePath(path string) error {
	volume := filepath.VolumeName(path)
	for _, component := range strings.Split(strings.TrimPrefix(path, volume), `\`) {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if !filepath.IsLocal(component) || strings.ContainsAny(component, `<>:"|?*`) || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return fmt.Errorf("unsafe Windows path component %q: %w", component, fs.ErrInvalid)
		}
		for _, character := range component {
			if character < 32 {
				return fmt.Errorf("invalid Windows path character: %w", fs.ErrInvalid)
			}
		}
	}
	return nil
}

func windowsPathPointer(path string) (*uint16, error) {
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	return windows.UTF16PtrFromString(path)
}

func privateSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;;FA;;;%s)(A;;FA;;;SY)", user.User.Sid, user.User.Sid))
}

func makePrivateDirectory(path string) error {
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return err
	}
	pointer, err := windowsPathPointer(path)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	if err := windows.CreateDirectory(pointer, &attributes); err != nil {
		return &os.PathError{Op: "create private directory", Path: path, Err: err}
	}
	if err := verifyPrivateDirectory(path); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func secureExistingDirectory(path string) (result error) {
	file, err := openWindowsObject(path, true, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	security, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if err := verifyWindowsOwner(security); err != nil {
		return err
	}
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return err
	}
	return verifyWindowsPrivacy(file)
}

func verifyPrivateDirectory(path string) (result error) {
	file, err := openWindowsObject(path, true, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	return verifyWindowsPrivacy(file)
}

func createPrivateFile(path string) (*os.File, error) {
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	pointer, err := windowsPathPointer(path)
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create private file", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	if err := verifyWindowsObject(file, false); err != nil {
		return nil, errors.Join(err, file.Close(), os.Remove(path))
	}
	if err := verifyWindowsPrivacy(file); err != nil {
		return nil, errors.Join(err, file.Close(), os.Remove(path))
	}
	return file, nil
}

func openPrivateFile(path string) (*os.File, error) {
	file, err := openWindowsObject(path, false, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	if err := verifyWindowsPrivacy(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func openWindowsObject(path string, directory bool, access uint32) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported private object type at %q: %w", path, fs.ErrInvalid)
	}
	pointer, err := windowsPathPointer(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pointer, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open private object", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	if err := verifyWindowsObject(file, directory); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func verifyWindowsObject(file *os.File, directory bool) error {
	handle := windows.Handle(file.Fd())
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return err
	}
	if fileType != windows.FILE_TYPE_DISK {
		return fmt.Errorf("private object is not a disk file: %w", fs.ErrInvalid)
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	if information.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 || (information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return fmt.Errorf("opened private object has unsafe attributes: %w", fs.ErrInvalid)
	}
	var flags uint32
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, &flags, nil, 0); err != nil {
		return err
	}
	if flags&windows.FILE_PERSISTENT_ACLS == 0 {
		return fmt.Errorf("filesystem does not support persistent ACLs: %w", errors.ErrUnsupported)
	}
	return nil
}

func verifyWindowsOwner(descriptor *windows.SECURITY_DESCRIPTOR) error {
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if owner == nil || !owner.IsValid() || !owner.Equals(user.User.Sid) {
		return fmt.Errorf("private object is not owned by the current user: %w", fs.ErrPermission)
	}
	return nil
}

func verifyWindowsPrivacy(file *os.File) error {
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return verifyWindowsDescriptor(descriptor)
}

func verifyWindowsDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) error {
	if err := verifyWindowsOwner(descriptor); err != nil {
		return err
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 || control&windows.SE_DACL_PRESENT == 0 {
		return fmt.Errorf("private DACL must be present and protected: %w", fs.ErrPermission)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil || dacl.AceCount < 1 || dacl.AceCount > 2 {
		return fmt.Errorf("unexpected private DACL: %w", fs.ErrPermission)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	ownerAllowed := false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &entry); err != nil {
			return err
		}
		if entry.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || entry.Header.AceFlags != 0 || entry.Mask != fileFullControl || entry.Header.AceSize < 16 {
			return fmt.Errorf("unexpected private ACE type, inheritance, or access: %w", fs.ErrPermission)
		}
		trustee := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
		if !trustee.IsValid() || trustee.Len() > int(entry.Header.AceSize)-8 {
			return fmt.Errorf("unverifiable private ACE trustee: %w", fs.ErrPermission)
		}
		if trustee.Equals(owner) {
			ownerAllowed = true
		} else if !trustee.IsWellKnown(windows.WinLocalSystemSid) {
			return fmt.Errorf("unexpected private ACE trustee: %w", fs.ErrPermission)
		}
	}
	if !ownerAllowed {
		return fmt.Errorf("private DACL does not grant owner access: %w", fs.ErrPermission)
	}
	return nil
}
