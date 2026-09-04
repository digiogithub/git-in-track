package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// revPrefix is the algorithm marker of a rev token.
const revPrefix = "sha256:"

// revHexLen is the number of hex characters kept from the digest. 64 bits is
// enough for "versions of one file that two clients hold at the same moment"
// (R-REV-2); this is not an adversarial setting.
const revHexLen = 16

// bom is the UTF-8 byte order mark, stripped before hashing and before parsing.
var bom = []byte{0xEF, 0xBB, 0xBF}

// Canonicalize returns the bytes a rev is computed over: no UTF-8 BOM, LF line
// endings and exactly one trailing LF (R-REV-1). It never returns the input
// slice, so the caller may keep mutating its own buffer.
func Canonicalize(data []byte) []byte {
	out := bytes.TrimPrefix(data, bom)
	out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
	out = bytes.TrimRight(out, "\n")
	buf := make([]byte, 0, len(out)+1)
	buf = append(buf, out...)
	return append(buf, '\n')
}

// ComputeRev returns the optimistic-concurrency token of a file: the SHA-256 of
// its canonical bytes, truncated to the first 16 hex characters and prefixed with
// the algorithm name, e.g. "sha256:9f2b1c7d0a4e5b31".
//
// rev is never stored in a file: it is recomputed on every read (R-REV-1).
func ComputeRev(data []byte) Rev {
	sum := sha256.Sum256(Canonicalize(data))
	return Rev(revPrefix + hex.EncodeToString(sum[:])[:revHexLen])
}

// Valid reports whether r has the shape produced by ComputeRev.
func (r Rev) Valid() bool {
	s := string(r)
	if !strings.HasPrefix(s, revPrefix) {
		return false
	}
	hexPart := s[len(revPrefix):]
	if len(hexPart) != revHexLen {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

// String returns the rev token as a plain string.
func (r Rev) String() string { return string(r) }
