package vfox

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// Checksum is one expected digest of a downloaded file.
type Checksum struct {
	Type  string // sha256 | md5 | sha1 | sha512
	Value string
}

// Pick returns the checksum a plugin supplied, preferring the same order vfox
// does (sha256, md5, sha1, sha512), or nil when none was given.
func (c *CheckSumItem) Pick() *Checksum {
	if c == nil {
		return nil
	}
	switch {
	case c.Sha256 != "":
		return &Checksum{"sha256", c.Sha256}
	case c.Md5 != "":
		return &Checksum{"md5", c.Md5}
	case c.Sha1 != "":
		return &Checksum{"sha1", c.Sha1}
	case c.Sha512 != "":
		return &Checksum{"sha512", c.Sha512}
	}
	return nil
}

// Verify streams the file through the digest and compares (case-insensitive).
func (c *Checksum) Verify(path string) error {
	if c == nil || c.Value == "" {
		return nil
	}
	var h hash.Hash
	switch c.Type {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	default:
		return fmt.Errorf("unsupported checksum type %q", c.Type)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(c.Value)) {
		return fmt.Errorf("%s checksum mismatch: expected %s, got %s", c.Type, c.Value, got)
	}
	return nil
}
