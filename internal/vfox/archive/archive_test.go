package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type entry struct {
	name    string
	content string
	dir     bool
	symlink string
}

func writeTarGz(t *testing.T, path string, entries []entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0755}
		switch {
		case e.dir:
			h.Typeflag = tar.TypeDir
		case e.symlink != "":
			h.Typeflag = tar.TypeSymlink
			h.Linkname = e.symlink
		default:
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.content))
		}
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	w.Close()
	f.Close()
}

func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected %s to exist: %v", p, err)
	}
}

func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Fatalf("expected %s to be absent", p)
	}
}

func TestTarStripsFirstComponent(t *testing.T) {
	src := filepath.Join(t.TempDir(), "go1.24.tar.gz")
	writeTarGz(t, src, []entry{
		{name: "go/", dir: true},
		{name: "go/bin/", dir: true},
		{name: "go/bin/go", content: "bin"},
		{name: "go/VERSION", content: "go1.24"},
		{name: "go/bin/golink", symlink: "go"},
	})
	dest := filepath.Join(t.TempDir(), "out")
	if err := Decompress(src, dest); err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	mustExist(t, filepath.Join(dest, "bin", "go"))
	mustExist(t, filepath.Join(dest, "VERSION"))
	mustNotExist(t, filepath.Join(dest, "go"))
	// symlink either real or copied
	data, err := os.ReadFile(filepath.Join(dest, "bin", "golink"))
	if err != nil || string(data) != "bin" {
		t.Fatalf("symlink fallback: %v %q", err, data)
	}
}

func TestTarRejectsTraversal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "evil.tgz")
	writeTarGz(t, src, []entry{{name: "pkg/../../evil", content: "x"}})
	if err := Decompress(src, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestZipStripsCommonRootOnly(t *testing.T) {
	dir := t.TempDir()
	rooted := filepath.Join(dir, "rooted.zip")
	writeZip(t, rooted, map[string]string{
		"node-v20/bin/node":    "n",
		"node-v20/README.md":   "r",
		"__MACOSX/._junk":      "j",
		"node-v20/lib/x/y.txt": "y",
	})
	dest := filepath.Join(dir, "out1")
	if err := Decompress(rooted, dest); err != nil {
		t.Fatalf("Decompress rooted: %v", err)
	}
	mustExist(t, filepath.Join(dest, "bin", "node"))
	mustExist(t, filepath.Join(dest, "lib", "x", "y.txt"))
	mustNotExist(t, filepath.Join(dest, "node-v20"))
	mustNotExist(t, filepath.Join(dest, "__MACOSX"))

	flat := filepath.Join(dir, "flat.zip")
	writeZip(t, flat, map[string]string{
		"zig.exe":     "z",
		"lib/std.zig": "s",
	})
	dest2 := filepath.Join(dir, "out2")
	if err := Decompress(flat, dest2); err != nil {
		t.Fatalf("Decompress flat: %v", err)
	}
	mustExist(t, filepath.Join(dest2, "zig.exe"))
	mustExist(t, filepath.Join(dest2, "lib", "std.zig"))
}

func TestNotArchiveAndBrokenSevenZip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.7z")
	os.WriteFile(p, []byte("not really 7z"), 0644)
	if err := Decompress(p, dir); err == nil {
		t.Fatal("garbage 7z must fail")
	}
	q := filepath.Join(dir, "x.msi")
	os.WriteFile(q, []byte("x"), 0644)
	if err := Decompress(q, dir); err != ErrNotArchive {
		t.Fatalf("expected ErrNotArchive, got %v", err)
	}
	if IsArchive("a.msi") || !IsArchive("a.tar.xz") || !IsArchive("a.7z") || !IsArchive("a.tar.zst") {
		t.Fatal("IsArchive")
	}
}

func writeTarZst(t *testing.T, path string, entries []entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0755}
		if e.dir {
			h.Typeflag = tar.TypeDir
		} else {
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			tw.Write([]byte(e.content))
		}
	}
	tw.Close()
	zw.Close()
	f.Close()
}

func TestTarZstStripsCommonRootOnly(t *testing.T) {
	dir := t.TempDir()
	rooted := filepath.Join(dir, "rooted.tar.zst")
	writeTarZst(t, rooted, []entry{
		{name: "pkg-1.0/", dir: true},
		{name: "pkg-1.0/bin/tool", content: "t"},
		{name: "pkg-1.0/README", content: "r"},
	})
	out := filepath.Join(dir, "out1")
	if err := Decompress(rooted, out); err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	mustExist(t, filepath.Join(out, "bin", "tool"))
	mustNotExist(t, filepath.Join(out, "pkg-1.0"))

	flat := filepath.Join(dir, "flat.tar.zst")
	writeTarZst(t, flat, []entry{
		{name: "a.txt", content: "a"},
		{name: "lib/b.txt", content: "b"},
	})
	out2 := filepath.Join(dir, "out2")
	if err := Decompress(flat, out2); err != nil {
		t.Fatalf("Decompress flat: %v", err)
	}
	mustExist(t, filepath.Join(out2, "a.txt"))
	mustExist(t, filepath.Join(out2, "lib", "b.txt"))
}
