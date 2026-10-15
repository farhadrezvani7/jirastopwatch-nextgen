package secret

import (
	"syscall"
	"unsafe"
)

var (
	advapi     = syscall.NewLazyDLL("advapi32.dll")
	procWrite  = advapi.NewProc("CredWriteW")
	procRead   = advapi.NewProc("CredReadW")
	procDelete = advapi.NewProc("CredDeleteW")
	procFree   = advapi.NewProc("CredFree")
)

const (
	credTypeGeneric  = 1
	credPersistLocal = 2
	errorNotFound    = 1168 // ERROR_NOT_FOUND
)

type credential struct {
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

func target() *uint16 { p, _ := syscall.UTF16PtrFromString(service + "/" + account); return p }

// Stored in Windows Credential Manager, encrypted for the current user.
func osBackend() Backend {
	return Backend{
		Get: func() (string, error) {
			var p *credential
			r, _, e := procRead.Call(uintptr(unsafe.Pointer(target())), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
			if r == 0 {
				if en, ok := e.(syscall.Errno); ok && en == errorNotFound {
					return "", ErrNotFound
				}
				return "", ErrUnavailable
			}
			defer procFree.Call(uintptr(unsafe.Pointer(p)))
			b := unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)
			return string(b), nil
		},
		Set: func(v string) error {
			blob := []byte(v)
			if len(blob) == 0 {
				return ErrUnavailable
			}
			user, _ := syscall.UTF16PtrFromString(account)
			c := credential{
				Type: credTypeGeneric, TargetName: target(), UserName: user,
				CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0], Persist: credPersistLocal,
			}
			if r, _, _ := procWrite.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
				return ErrUnavailable
			}
			return nil
		},
		Delete: func() error {
			r, _, e := procDelete.Call(uintptr(unsafe.Pointer(target())), credTypeGeneric, 0)
			if r == 0 {
				if en, ok := e.(syscall.Errno); ok && en == errorNotFound {
					return nil
				}
				return ErrUnavailable
			}
			return nil
		},
	}
}
