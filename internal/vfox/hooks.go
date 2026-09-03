package vfox

// Hook contexts and results. The json tags ARE the plugin contract — they are
// the field names Lua hooks read and write — and mirror vfox's
// internal/plugin/model.go exactly.

// PreInstallPackageItem describes one downloadable package (the main SDK or
// an "addition" such as npm shipped next to Node).
type PreInstallPackageItem struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Path    string            `json:"url"`     // remote URL or local path; "" = nothing to download
	Headers map[string]string `json:"headers"` // extra request headers
	Note    string            `json:"note"`
	*CheckSumItem
}

// CheckSumItem carries whichever checksum the plugin provided.
type CheckSumItem struct {
	Sha256 string `json:"sha256"`
	Sha512 string `json:"sha512"`
	Sha1   string `json:"sha1"`
	Md5    string `json:"md5"`
}

// Label is "name@version" for messages.
func (p *PreInstallPackageItem) Label() string {
	if p.Version == "" {
		return p.Name
	}
	return p.Name + "@" + p.Version
}

type AvailableHookCtx struct {
	Args []string `json:"args"`
}

type AvailableHookResultItem struct {
	Version  string                   `json:"version"`
	Note     string                   `json:"note"`
	Addition []*PreInstallPackageItem `json:"addition"`
}

type PreInstallHookCtx struct {
	Version string `json:"version"`
}

type PreInstallHookResult struct {
	*PreInstallPackageItem
	Addition []*PreInstallPackageItem `json:"addition"`
}

// InstalledPackageItem is what hooks after installation see for each package.
type InstalledPackageItem struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Note    string `json:"note"`
}

type PostInstallHookCtx struct {
	RootPath string                           `json:"rootPath"`
	SdkInfo  map[string]*InstalledPackageItem `json:"sdkInfo"`
}

type EnvKeysHookCtx struct {
	Main    *InstalledPackageItem            `json:"main"`
	Path    string                           `json:"path"`
	SdkInfo map[string]*InstalledPackageItem `json:"sdkInfo"`
}

type EnvKeysHookResultItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PreUseHookCtx struct {
	Cwd             string                           `json:"cwd"`
	Scope           string                           `json:"scope"`
	Version         string                           `json:"version"`
	PreviousVersion string                           `json:"previousVersion"`
	InstalledSdks   map[string]*InstalledPackageItem `json:"installedSdks"`
}

type PreUseHookResult struct {
	Version string `json:"version"`
}

type ParseLegacyFileHookCtx struct {
	Filepath             string          `json:"filepath"`
	Filename             string          `json:"filename"`
	GetInstalledVersions func() []string `json:"getInstalledVersions"`
	// Strategy: "latest_installed" | "latest_available" | "specified" (default)
	Strategy string `json:"strategy"`
}

type ParseLegacyFileResult struct {
	Version string `json:"version"`
}

type PreUninstallHookCtx struct {
	Main    *InstalledPackageItem            `json:"main"`
	SdkInfo map[string]*InstalledPackageItem `json:"sdkInfo"`
}

// RuntimeInfo is the RUNTIME global.
type RuntimeInfo struct {
	OsType        string `json:"osType"`
	ArchType      string `json:"archType"`
	Version       string `json:"version"`
	PluginDirPath string `json:"pluginDirPath"`
}

// Metadata is the PLUGIN table as declared in metadata.lua (or main.lua).
type Metadata struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	Description       string   `json:"description"`
	UpdateUrl         string   `json:"updateUrl"`
	ManifestUrl       string   `json:"manifestUrl"`
	Homepage          string   `json:"homepage"`
	License           string   `json:"license"`
	MinRuntimeVersion string   `json:"minRuntimeVersion"`
	Notes             []string `json:"notes"`
	LegacyFilenames   []string `json:"legacyFilenames"`
}

// hookSpec lists the hook functions vfox knows, their file names under
// hooks/, and whether a plugin must provide them.
type hookSpec struct {
	Name     string
	File     string
	Required bool
}

var hookSpecs = []hookSpec{
	{"Available", "available", true},
	{"PreInstall", "pre_install", true},
	{"EnvKeys", "env_keys", true},
	{"PostInstall", "post_install", false},
	{"PreUse", "pre_use", false},
	{"ParseLegacyFile", "parse_legacy_file", false},
	{"PreUninstall", "pre_uninstall", false},
}
