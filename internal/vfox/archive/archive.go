// Package archive extracts the archive formats vfox plugins hand DevBox,
// replicating vfox's layout rules so that plugin EnvKeys paths stay valid:
//   - tar.* : the first path component of every entry is dropped unconditionally
//   - zip   : the single common root folder (if every entry shares one) is dropped
//
// The strip semantics are ported from github.com/version-fox/vfox
// internal/shared/util/decompressor.go (Apache-2.0) — see THIRD_PARTY_NOTICES.md.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// ErrNotArchive is returned by Decompress for files that are not a supported archive.
var ErrNotArchive = errors.New("not an archive")

// ErrUnsupported is returned for archive formats DevBox recognises but cannot extract yet.
var ErrUnsupported = errors.New("archive format not supported yet")

// IsArchive reports whether the file name has an archive extension DevBox can extract.
func IsArchive(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	for _, s := range []string{".tar.gz", ".tgz", ".tar.xz", ".tar.bz2", ".tbz2", ".zip"} {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}

// IsKnownArchive is IsArchive plus the formats that are recognised but unsupported (7z, zstd).
func IsKnownArchive(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	return IsArchive(n) || strings.HasSuffix(n, ".7z") || strings.HasSuffix(n, ".tar.zst") || strings.HasSuffix(n, ".tzst")
}

// Decompress extracts src into dest (created if missing).
func Decompress(src, dest string) error {
	n := strings.ToLower(filepath.Base(src))
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	switch {
	case strings.HasSuffix(n, ".tar.gz") || strings.HasSuffix(n, ".tgz"):
		return extractTar(src, dest, func(r io.Reader) (io.Reader, func(), error) {
			gz, err := gzip.NewReader(r)
			if err != nil {
				return nil, nil, err
			}
			return gz, func() { gz.Close() }, nil
		})
	case strings.HasSuffix(n, ".tar.xz"):
		return extractTar(src, dest, func(r io.Reader) (io.Reader, func(), error) {
			xr, err := xz.NewReader(bufio.NewReader(r))
			if err != nil {
				return nil, nil, err
			}
			return xr, func() {}, nil
		})
	case strings.HasSuffix(n, ".tar.bz2") || strings.HasSuffix(n, ".tbz2"):
		return extractTar(src, dest, func(r io.Reader) (io.Reader, func(), error) {
			return bzip2.NewReader(r), func() {}, nil
		})
	case strings.HasSuffix(n, ".zip"):
		return extractZip(src, dest)
	case strings.HasSuffix(n, ".7z"):
		return fmt.Errorf("%w: 7z (%s)", ErrUnsupported, filepath.Base(src))
	case strings.HasSuffix(n, ".tar.zst") || strings.HasSuffix(n, ".tzst"):
		return fmt.Errorf("%w: zstd (%s)", ErrUnsupported, filepath.Base(src))
	}
	return ErrNotArchive
}

type link struct {
	target, name string
	hard         bool
}

// safeTarget joins the (already stripped) entry name onto dest, refusing
// anything that escapes it.
func safeTarget(dest, fname string) (string, error) {
	fname = strings.TrimPrefix(fname, "./")
	clean := filepath.Clean(strings.ReplaceAll(fname, "\\", "/"))
	if clean == "." || clean == "" {
		return "", nil
	}
	if filepath.IsAbs(clean) || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("archive entry %q is outside destination", fname)
	}
	return filepath.Join(dest, clean), nil
}

func extractTar(src, dest string, wrap func(io.Reader) (io.Reader, func(), error)) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	r, closeFn, err := wrap(file)
	if err != nil {
		return err
	}
	defer closeFn()

	tr := tar.NewReader(r)
	var links []link
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header == nil {
			continue
		}
		// vfox drops the first path component of every entry (archives are
		// expected to carry a single top-level directory).
		parts := strings.Split(strings.ReplaceAll(header.Name, "\\", "/"), "/")
		if len(parts) > 1 {
			parts = parts[1:]
		}
		target, err := safeTarget(dest, strings.Join(parts, "/"))
		if err != nil {
			return err
		}
		if target == "" {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & os.ModePerm
			if mode == 0 {
				mode = 0644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			links = append(links, link{target: header.Linkname, name: target})
		case tar.TypeLink:
			lp := strings.Split(strings.ReplaceAll(header.Linkname, "\\", "/"), "/")
			if len(lp) > 1 {
				lp = lp[1:]
			}
			lt, err := safeTarget(dest, strings.Join(lp, "/"))
			if err != nil {
				return err
			}
			links = append(links, link{target: lt, name: target, hard: true})
		}
	}
	return createLinks(dest, links)
}

// createLinks makes symlinks after all regular entries exist. On Windows (or
// wherever symlink creation fails) the link target is copied instead, so a
// Unix-shaped payload still yields working files.
func createLinks(dest string, links []link) error {
	for _, l := range links {
		if err := os.MkdirAll(filepath.Dir(l.name), 0755); err != nil {
			return err
		}
		_ = os.RemoveAll(l.name)
		if l.hard {
			if err := os.Link(l.target, l.name); err != nil {
				if err := copyPath(l.target, l.name); err != nil {
					return fmt.Errorf("%s: %w", l.name, err)
				}
			}
			continue
		}
		if err := os.Symlink(l.target, l.name); err == nil {
			continue
		}
		resolved := l.target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(l.name), resolved)
		}
		if rel, err := filepath.Rel(dest, resolved); err != nil || strings.HasPrefix(rel, "..") {
			continue // points outside the archive — nothing sensible to copy
		}
		if _, err := os.Stat(resolved); err != nil {
			continue // dangling
		}
		if err := copyPath(resolved, l.name); err != nil {
			return fmt.Errorf("%s: %w", l.name, err)
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			out := filepath.Join(dst, rel)
			if info.IsDir() {
				return os.MkdirAll(out, 0755)
			}
			return copyFile(p, out, info.Mode())
		})
	}
	return copyFile(src, dst, fi.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode&os.ModePerm|0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func skipZipEntry(normalized string) bool {
	return strings.HasPrefix(normalized, ".DS_Store") || strings.HasPrefix(normalized, "__MACOSX")
}

// findRootFolderInZip returns the single top-level folder every entry shares,
// or "" when entries live at the root (or under different folders).
func findRootFolderInZip(r *zip.ReadCloser) string {
	var first string
	for _, f := range r.File {
		normalized := strings.ReplaceAll(f.Name, "\\", "/")
		if skipZipEntry(normalized) {
			continue
		}
		cur := strings.Split(normalized, "/")[0]
		if first != "" && first != cur {
			return ""
		}
		if first == "" {
			first = cur
		}
	}
	return first
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	root := findRootFolderInZip(r)
	var links []link
	for _, f := range r.File {
		normalized := strings.ReplaceAll(f.Name, "\\", "/")
		if skipZipEntry(normalized) {
			continue
		}
		parts := strings.Split(normalized, "/")
		if len(parts) > 1 && root != "" {
			parts = parts[1:]
		}
		fname := strings.Join(parts, "/")
		target, err := safeTarget(dest, fname)
		if err != nil {
			return err
		}
		if target == "" {
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(fname, "/") {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			buf := new(bytes.Buffer)
			_, err := io.Copy(buf, rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("%s: reading symlink target: %v", f.Name, err)
			}
			links = append(links, link{target: strings.TrimSpace(buf.String()), name: target})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			rc.Close()
			return err
		}
		mode := f.Mode() & os.ModePerm
		if mode == 0 {
			mode = 0644
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return createLinks(dest, links)
}
