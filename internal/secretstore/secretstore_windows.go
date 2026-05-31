//go:build windows

package secretstore

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect stores a value in a DPAPI-protected blob tied to the current Windows
// user. The returned string can be persisted in SQLite without exposing the
// plaintext token or private key to a casual database read.
func Protect(label string, value string) (string, error) {
	if value == "" || alreadyStored(value) {
		return value, nil
	}
	blob := dataBlob([]byte(value))
	var out windows.DataBlob
	name, err := windows.UTF16PtrFromString("TaskFerry " + label)
	if err != nil {
		return "", err
	}
	if err := windows.CryptProtectData(blob, name, nil, 0, nil, 0, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return encode(dpapiPrefix, unsafe.Slice(out.Data, out.Size)), nil
}

// Unprotect reads DPAPI-protected values. Plain values are returned unchanged so
// older local databases remain readable and can be re-saved into protected form.
func Unprotect(value string) (string, error) {
	if raw, ok, err := decode(value, dpapiPrefix); ok || err != nil {
		if err != nil {
			return "", err
		}
		blob := dataBlob(raw)
		var out windows.DataBlob
		if err := windows.CryptUnprotectData(blob, nil, nil, 0, nil, 0, &out); err != nil {
			return "", err
		}
		defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
		return string(unsafe.Slice(out.Data, out.Size)), nil
	}
	if raw, ok, err := decode(value, localPrefix); ok || err != nil {
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return value, nil
}

func dataBlob(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}
