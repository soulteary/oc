package cmd

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privatePartialDirectory(os.FileInfo) bool { return true }

// Windows ignores POSIX mode bits. Check ownership and restrict the opened
// directory's ACL before placing credentials or object data inside it.
func securePartialDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	handle, err := openPartialHandle(windows.Handle(dir.Fd()), ".", windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	token := windows.GetCurrentProcessToken()
	var size uint32
	err = windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size)
	if err != windows.ERROR_INSUFFICIENT_BUFFER {
		return fmt.Errorf("query staging owner: %w", err)
	}
	buf := make([]byte, size)
	if err = windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], size, &size); err != nil {
		return err
	}
	expected := *(**windows.SID)(unsafe.Pointer(&buf[0]))
	if !windows.EqualSid(owner, expected) {
		return fmt.Errorf("staging directory belongs to another identity")
	}
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	aclSD, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	acl, _, err := aclSD.DACL()
	if err != nil {
		return err
	}
	err = windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	runtime.KeepAlive(buf)
	return err
}

func openPartialHandle(parent windows.Handle, name string, access, options uint32) (windows.Handle, error) {
	// NT relative opens use an empty name to reopen the directory itself.
	if name == "." {
		name = ""
	}
	unicode, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: unicode, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &attributes, &windows.IO_STATUS_BLOCK{}, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		options|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if status, ok := err.(windows.NTStatus); ok {
		err = status.Errno()
	}
	return handle, err
}

func renameLocalPartial(stage, parent *os.Root, target string) error {
	source, err := stage.Open(".")
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer destination.Close()
	handle, err := openPartialHandle(windows.Handle(source.Fd()), partialDataName, windows.DELETE, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	name, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	type renameInfo struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	length := unsafe.Offsetof(renameInfo{}.FileName) + uintptr(len(name))*2
	buf := make([]byte, length)
	info := (*renameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.RootDirectory = windows.Handle(destination.Fd())
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	// Match os.Rename: prefer POSIX replacement, then support older filesystems.
	const fileRenameInformationEx = 65
	err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, &buf[0], uint32(len(buf)), fileRenameInformationEx)
	if err != nil {
		info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS
		err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, &buf[0], uint32(len(buf)), windows.FileRenameInformation)
	}
	if status, ok := err.(windows.NTStatus); ok {
		err = status.Errno()
	}
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: partialDataName, New: target, Err: err}
	}
	return nil
}
