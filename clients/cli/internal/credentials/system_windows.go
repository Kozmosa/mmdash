//go:build windows

package credentials

import (
	"errors"
	"syscall"
	"unsafe"
)

const (
	credentialTypeGeneric                       = 1
	credentialPersistLocalMachine               = 2
	errorNotFound                 syscall.Errno = 1168
)

var (
	advapi32   = syscall.NewLazyDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func platformSet(profile string, value string) error {
	target, err := syscall.UTF16PtrFromString("mmdash/" + profile)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString("mmdash-cli")
	if err != nil {
		return err
	}
	blob := []byte(value)
	credential := nativeCredential{Type: credentialTypeGeneric, TargetName: target, CredentialBlobSize: uint32(len(blob)), Persist: credentialPersistLocalMachine, UserName: user}
	if len(blob) > 0 {
		credential.CredentialBlob = &blob[0]
	}
	result, _, callErr := credWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return callErr
	}
	return nil
}

func platformGet(profile string) (string, error) {
	target, err := syscall.UTF16PtrFromString("mmdash/" + profile)
	if err != nil {
		return "", err
	}
	var credential *nativeCredential
	result, _, callErr := credRead.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0, uintptr(unsafe.Pointer(&credential)))
	if result == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", ErrNotFound
		}
		return "", callErr
	}
	defer credFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.CredentialBlobSize == 0 {
		return "", nil
	}
	value := unsafe.Slice(credential.CredentialBlob, credential.CredentialBlobSize)
	return string(value), nil
}

func platformDelete(profile string) error {
	target, err := syscall.UTF16PtrFromString("mmdash/" + profile)
	if err != nil {
		return err
	}
	result, _, callErr := credDelete.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	if result == 0 && !errors.Is(callErr, errorNotFound) {
		return callErr
	}
	return nil
}
