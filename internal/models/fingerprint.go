package models

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func NewFingerprint(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:12])
}
