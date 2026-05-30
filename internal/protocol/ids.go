package protocol

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	enc := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
	return prefix + "_" + enc
}
