package modules

import (
	"os"
	"path/filepath"

	"DevBox/internal/platform"

	lua "github.com/yuin/gopher-lua"
)

// preloadArchiver registers `require("archiver")`:
//
//	local err = archiver.decompress(srcFile, destDir)
func preloadArchiver(L *lua.LState, h *Host) {
	L.PreloadModule("archiver", func(L *lua.LState) int {
		t := L.NewTable()
		L.SetFuncs(t, map[string]lua.LGFunction{
			"decompress": func(L *lua.LState) int {
				src := L.CheckString(1)
				dest := L.CheckString(2)
				if h.Decompress == nil {
					L.Push(lua.LString("archiver is not available"))
					return 1
				}
				if err := h.Decompress(src, dest); err != nil {
					L.Push(lua.LString(err.Error()))
					return 1
				}
				return 0
			},
		})
		L.Push(t)
		return 1
	})
}

// preloadFile registers `require("file")`:
//
//	file.symlink(src, dest) -- both relative to the SDK root, like vfox
//
// On Windows a directory symlink that cannot be created (no Developer Mode)
// falls back to an NTFS junction.
func preloadFile(L *lua.LState, h *Host) {
	L.PreloadModule("file", func(L *lua.LState) int {
		t := L.NewTable()
		L.SetFuncs(t, map[string]lua.LGFunction{
			"symlink": func(L *lua.LState) int {
				src := filepath.Join(h.SymlinkRoot, L.CheckString(1))
				dest := filepath.Join(h.SymlinkRoot, L.CheckString(2))
				err := os.Symlink(src, dest)
				if err != nil {
					if fi, statErr := os.Stat(src); statErr == nil && fi.IsDir() {
						err = platform.LinkDir(dest, src)
					}
				}
				if err != nil {
					L.RaiseError("%s", err.Error())
					return 0
				}
				L.Push(lua.LTrue)
				return 1
			},
		})
		L.Push(t)
		return 1
	})
}
