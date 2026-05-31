package secretstore

import (
	"encoding/base64"
	"strings"
)

const (
	dpapiPrefix = "dpapi:"
	localPrefix = "local:"
)

func alreadyStored(value string) bool {
	return strings.HasPrefix(value, dpapiPrefix) || strings.HasPrefix(value, localPrefix)
}

func encode(prefix string, value []byte) string {
	return prefix + base64.RawURLEncoding.EncodeToString(value)
}

func decode(value string, prefix string) ([]byte, bool, error) {
	if !strings.HasPrefix(value, prefix) {
		return nil, false, nil
	}
	out, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return out, true, err
}
