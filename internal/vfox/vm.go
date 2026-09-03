package vfox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"DevBox/internal/vfox/archive"
	"DevBox/internal/vfox/codec"
	"DevBox/internal/vfox/modules"

	lua "github.com/yuin/gopher-lua"
)

// Errors surfaced from hook calls.
var (
	ErrNoResult    = errors.New("plugin returned no result")
	ErrHookMissing = errors.New("hook is not implemented by this plugin")
)

const (
	pluginObjKey = "PLUGIN"
	osTypeKey    = "OS_TYPE"
	archTypeKey  = "ARCH_TYPE"
	runtimeKey   = "RUNTIME"
)

var pluginNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]*$`)

// vm is one Lua state with a loaded plugin. It mirrors vfox's CreateLuaPlugin:
// full standard library, the vfox modules, package.path restricted to the
// plugin, scripts executed, then the globals every hook relies on.
type vm struct {
	L      *lua.LState
	plugin *lua.LTable
	dir    string
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func newVM(dir string, host *modules.Host) (*vm, *Metadata, error) {
	L := lua.NewState()
	ok := false
	defer func() {
		if !ok {
			L.Close()
		}
	}()

	if host.Decompress == nil {
		host.Decompress = archive.Decompress
	}
	if err := modules.Preload(L, host); err != nil {
		return nil, nil, err
	}

	pkg, _ := L.GetGlobal("package").(*lua.LTable)
	setPath := func(paths ...string) {
		if pkg != nil {
			pkg.RawSetString("path", lua.LString(strings.Join(paths, ";")))
		}
	}

	mainPath := filepath.Join(dir, "main.lua")
	if fileExists(mainPath) {
		// Legacy single-file layout.
		setPath(filepath.Join(dir, "?.lua"))
		if err := L.DoFile(mainPath); err != nil {
			return nil, nil, err
		}
	} else {
		setPath(filepath.Join(dir, "hooks", "?.lua"), filepath.Join(dir, "lib", "?.lua"))
		metadataPath := filepath.Join(dir, "metadata.lua")
		if !fileExists(metadataPath) {
			return nil, nil, fmt.Errorf("plugin invalid: metadata.lua not found in %s", dir)
		}
		if err := L.DoFile(metadataPath); err != nil {
			return nil, nil, fmt.Errorf("failed to load metadata.lua: %w", err)
		}
		for _, h := range hookSpecs {
			hp := filepath.Join(dir, "hooks", h.File+".lua")
			if !h.Required && !fileExists(hp) {
				continue
			}
			if err := L.DoFile(hp); err != nil {
				return nil, nil, fmt.Errorf("failed to load [%s] hook: %w", h.Name, err)
			}
		}
	}

	// Set after the scripts ran so a plugin cannot overwrite them.
	L.SetGlobal(osTypeKey, lua.LString(OSType()))
	L.SetGlobal(archTypeKey, lua.LString(ArchType()))
	rt, err := codec.Marshal(L, RuntimeInfo{
		OsType:        OSType(),
		ArchType:      ArchType(),
		Version:       CompatVersion,
		PluginDirPath: dir,
	})
	if err != nil {
		return nil, nil, err
	}
	L.SetGlobal(runtimeKey, rt)

	pluginObj, isTable := L.GetGlobal(pluginObjKey).(*lua.LTable)
	if !isTable {
		return nil, nil, errors.New("plugin invalid: PLUGIN table not found")
	}
	meta := &Metadata{}
	if err := codec.Unmarshal(pluginObj, meta); err != nil {
		return nil, nil, err
	}
	if !pluginNameRe.MatchString(meta.Name) {
		return nil, nil, fmt.Errorf("plugin invalid: bad name %q", meta.Name)
	}
	for _, h := range hookSpecs {
		if h.Required && pluginObj.RawGetString(h.Name) == lua.LNil {
			return nil, nil, fmt.Errorf("plugin invalid: required hook %s is missing", h.Name)
		}
	}

	nav, err := codec.Marshal(L, codec.Navigator{UserAgent: UserAgent(meta.Name, meta.Version)})
	if err != nil {
		return nil, nil, err
	}
	L.SetGlobal(codec.NavigatorObjKey, nav)

	ok = true
	return &vm{L: L, plugin: pluginObj, dir: dir}, meta, nil
}

func (v *vm) hasHook(name string) bool {
	return v.plugin.RawGetString(name) != lua.LNil
}

// call invokes PLUGIN:<name>(ctx). When out is nil the result is ignored;
// otherwise a nil/absent result is ErrNoResult and a table is decoded into out.
func (v *vm) call(name string, ctx any, out any, timeout time.Duration) error {
	fn, ok := v.plugin.RawGetString(name).(*lua.LFunction)
	if !ok {
		return ErrHookMissing
	}
	ctxTable, err := codec.Marshal(v.L, ctx)
	if err != nil {
		return err
	}
	if timeout > 0 {
		c, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v.L.SetContext(c)
		defer v.L.RemoveContext()
	}
	if err := v.L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, v.plugin, ctxTable); err != nil {
		return errors.New(strings.TrimSpace(err.Error()))
	}
	ret := v.L.Get(-1)
	v.L.Pop(1)
	if out == nil {
		return nil
	}
	if ret == lua.LNil {
		return ErrNoResult
	}
	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return fmt.Errorf("%s returned %s, expected a table", name, ret.Type().String())
	}
	if err := codec.Unmarshal(tbl, out); err != nil {
		return fmt.Errorf("failed to decode %s result: %w", name, err)
	}
	return nil
}

func (v *vm) close() {
	if v != nil && v.L != nil {
		v.L.Close()
	}
}
