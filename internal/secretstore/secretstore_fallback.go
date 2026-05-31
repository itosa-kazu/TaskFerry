//go:build !windows

package secretstore

import "errors"

// Protect falls back to tagged local encoding on platforms where TaskFerry has
// not yet wired an OS keychain backend. This avoids raw secret strings in the
// SQLite settings table, but it is not a security boundary.
func Protect(label string, value string) (string, error) {
	if value == "" || alreadyStored(value) {
		return value, nil
	}
	return encode(localPrefix, []byte(value)), nil
}

func Unprotect(value string) (string, error) {
	if raw, ok, err := decode(value, localPrefix); ok || err != nil {
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	if raw, ok, err := decode(value, dpapiPrefix); ok || err != nil {
		if err != nil {
			return "", err
		}
		_ = raw
		return "", errors.New("dpapi-protected secret cannot be decrypted on this platform")
	}
	return value, nil
}
