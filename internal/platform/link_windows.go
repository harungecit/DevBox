//go:build windows

package platform

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// ioReparseTagMountPoint identifies an NTFS junction (mount point) reparse tag.
const ioReparseTagMountPoint = 0xA0000003

// LinkDir creates an NTFS junction at `link` pointing at the directory
// `target`. Junctions need no elevation or Developer Mode (unlike symlinks),
// resolve for every process on the machine, and deleting one never touches
// the target directory (verified: os.Remove/os.RemoveAll unlink the junction
// itself without descending into it).
func (w *windowsPlatform) LinkDir(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	fi, err := os.Stat(absTarget)
	if err != nil {
		return fmt.Errorf("link target not found: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("link target is not a directory: %s", absTarget)
	}

	// A junction is a reparse point set on an (empty) directory.
	if err := os.Mkdir(link, 0755); err != nil {
		return err
	}

	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		os.Remove(link)
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		os.Remove(link)
		return err
	}
	defer windows.CloseHandle(h)

	// REPARSE_DATA_BUFFER for a mount point: header (tag, data length,
	// reserved) + substitute name (NT namespace path) + print name.
	subst := utf16.Encode([]rune(`\??\` + absTarget))
	print := utf16.Encode([]rune(absTarget))
	pathBuf := append(append(append([]uint16{}, subst...), 0), append(print, 0)...)

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(ioReparseTagMountPoint))
	binary.Write(buf, binary.LittleEndian, uint16(8+len(pathBuf)*2)) // reparse data length
	binary.Write(buf, binary.LittleEndian, uint16(0))                // reserved
	binary.Write(buf, binary.LittleEndian, uint16(0))                // substitute name offset
	binary.Write(buf, binary.LittleEndian, uint16(len(subst)*2))     // substitute name length
	binary.Write(buf, binary.LittleEndian, uint16(len(subst)*2+2))   // print name offset
	binary.Write(buf, binary.LittleEndian, uint16(len(print)*2))     // print name length
	binary.Write(buf, binary.LittleEndian, pathBuf)

	var ret uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT,
		&buf.Bytes()[0], uint32(buf.Len()), nil, 0, &ret, nil)
	if err != nil {
		os.Remove(link)
		return fmt.Errorf("junction creation failed: %w", err)
	}
	return nil
}
