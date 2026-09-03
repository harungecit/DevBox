package vfox

import (
	"errors"
	"io"
	"sync"
	"time"

	"DevBox/internal/vfox/modules"
)

// Hook timeouts. PostInstall has none: it may compile from source.
// Variables so tests can shorten them.
var (
	timeoutAvailable   = 60 * time.Second
	timeoutPreInstall  = 120 * time.Second
	timeoutEnvKeys     = 30 * time.Second
	timeoutPreUse      = 30 * time.Second
	timeoutLegacyFile  = 30 * time.Second
	timeoutPreUninstal = 60 * time.Second
)

// Plugin is a loaded vfox plugin. One Lua state serves all hooks, guarded by
// a mutex; after an error or timeout the state is discarded and rebuilt on
// the next call (a plugin's in-memory caches are lost, nothing else).
type Plugin struct {
	Dir  string
	Meta Metadata

	mu   sync.Mutex
	vm   *vm
	host *modules.Host
}

// Load parses a plugin directory (metadata.lua + hooks/, or legacy main.lua)
// and validates it. No hook runs.
func Load(dir string) (*Plugin, error) {
	p := &Plugin{Dir: dir, host: &modules.Host{Dir: dir}}
	v, meta, err := newVM(dir, p.host)
	if err != nil {
		return nil, err
	}
	p.vm = v
	p.Meta = *meta
	return p, nil
}

// SetExecContext routes os.execute / io.popen children of subsequent hooks:
// working dir, environment, log sink and a per-line callback.
func (p *Plugin) SetExecContext(dir string, env []string, log io.Writer, onOutput func(string)) {
	p.host.SetExecContext(dir, env, log, onOutput)
}

// SetSymlinkRoot sets the base directory for file.symlink (the SDK root).
func (p *Plugin) SetSymlinkRoot(root string) {
	p.host.SymlinkRoot = root
}

func (p *Plugin) ensureVM() error {
	if p.vm != nil {
		return nil
	}
	v, meta, err := newVM(p.Dir, p.host)
	if err != nil {
		return err
	}
	p.vm = v
	p.Meta = *meta
	return nil
}

// HasHook reports whether the plugin implements an optional hook.
func (p *Plugin) HasHook(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ensureVM() != nil {
		return false
	}
	return p.vm.hasHook(name)
}

func (p *Plugin) run(hook string, ctx any, out any, timeout time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureVM(); err != nil {
		return pluginError(p.Meta.Name, err)
	}
	err := p.vm.call(hook, ctx, out, timeout)
	if err != nil && !errors.Is(err, ErrNoResult) && !errors.Is(err, ErrHookMissing) {
		// A failed or interrupted state is not trusted again.
		p.host.KillRunning()
		p.vm.close()
		p.vm = nil
		return pluginError(p.Meta.Name, err)
	}
	return err
}

// Available lists the versions the plugin can install (newest first, as the
// plugin orders them).
func (p *Plugin) Available(args []string) ([]*AvailableHookResultItem, error) {
	if args == nil {
		args = []string{}
	}
	var out []*AvailableHookResultItem
	if err := p.run("Available", &AvailableHookCtx{Args: args}, &out, timeoutAvailable); err != nil {
		return nil, err
	}
	return out, nil
}

// PreInstall resolves a version (aliases like "latest" included) into
// download instructions.
func (p *Plugin) PreInstall(version string) (*PreInstallHookResult, error) {
	out := &PreInstallHookResult{}
	if err := p.run("PreInstall", &PreInstallHookCtx{Version: version}, out, timeoutPreInstall); err != nil {
		return nil, err
	}
	if out.PreInstallPackageItem == nil || out.Version == "" {
		return nil, pluginError(p.Meta.Name, errors.New("no installable version found for "+version))
	}
	return out, nil
}

// PostInstall runs the plugin's post-install steps (compilation, wrappers…).
func (p *Plugin) PostInstall(ctx *PostInstallHookCtx) error {
	err := p.run("PostInstall", ctx, nil, 0)
	if errors.Is(err, ErrHookMissing) {
		return nil
	}
	return err
}

// EnvKeys asks which PATH entries and variables an installed version needs.
func (p *Plugin) EnvKeys(ctx *EnvKeysHookCtx) ([]*EnvKeysHookResultItem, error) {
	var out []*EnvKeysHookResultItem
	if err := p.run("EnvKeys", ctx, &out, timeoutEnvKeys); err != nil {
		return nil, err
	}
	return out, nil
}

// PreUse lets the plugin rewrite a version before it is activated (optional).
func (p *Plugin) PreUse(ctx *PreUseHookCtx) (*PreUseHookResult, error) {
	out := &PreUseHookResult{}
	err := p.run("PreUse", ctx, out, timeoutPreUse)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParseLegacyFile reads a .nvmrc-style file (optional hook).
func (p *Plugin) ParseLegacyFile(ctx *ParseLegacyFileHookCtx) (*ParseLegacyFileResult, error) {
	if ctx.GetInstalledVersions == nil {
		ctx.GetInstalledVersions = func() []string { return []string{} }
	}
	if ctx.Strategy == "" {
		ctx.Strategy = "specified"
	}
	out := &ParseLegacyFileResult{}
	if err := p.run("ParseLegacyFile", ctx, out, timeoutLegacyFile); err != nil {
		return nil, err
	}
	return out, nil
}

// PreUninstall runs cleanup before a version is removed (optional).
func (p *Plugin) PreUninstall(ctx *PreUninstallHookCtx) error {
	err := p.run("PreUninstall", ctx, nil, timeoutPreUninstal)
	if errors.Is(err, ErrHookMissing) {
		return nil
	}
	return err
}

// Close releases the Lua state.
func (p *Plugin) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vm.close()
	p.vm = nil
}
