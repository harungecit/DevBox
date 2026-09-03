package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Framework describes one entry of DevBox's framework catalog: how to
// recognise it in a folder, which runtime it needs, whether it runs its own
// HTTP server (app server) or is served by a web server, and how to start it.
type Framework struct {
	Name      string `json:"name"`
	Runtime   string `json:"runtime"`   // php | node | python | go | rust | static | a plugin runtime (crystal…)
	AppServer bool   `json:"appServer"` // true → dev server + front-door, false → nginx/apache/caddy/frankenphp vhost
	Port      int    `json:"port"`      // preferred dev-server port (app servers only)

	// detect reports whether projectPath looks like this framework. Entries are
	// evaluated in catalog order, so specific frameworks must precede generic
	// ones (Laravel before PHP, Next.js before Node…).
	detect func(d *detector) bool
	// start builds the dev-server command; nil falls back to the runtime default.
	start func(d *detector, port int) (string, []string)
}

// detector caches the files DevBox looks at while probing a folder.
type detector struct {
	path    string
	pkgRaw  string            // package.json contents
	pkgDeps map[string]string // dependencies + devDependencies
	pkgScr  map[string]string // scripts
	compRaw string            // composer.json contents
	goMod   string            // go.mod contents
	cargo   string            // Cargo.toml contents
	pyDeps  string            // requirements.txt + pyproject.toml + Pipfile, lower-cased
	shard   string            // shard.yml contents (Crystal)
}

func newDetector(path string) *detector {
	d := &detector{path: path, pkgDeps: map[string]string{}, pkgScr: map[string]string{}}
	if b, err := os.ReadFile(filepath.Join(path, "package.json")); err == nil {
		d.pkgRaw = string(b)
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
			Scripts         map[string]string `json:"scripts"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			for k, v := range pkg.Dependencies {
				d.pkgDeps[k] = v
			}
			for k, v := range pkg.DevDependencies {
				d.pkgDeps[k] = v
			}
			d.pkgScr = pkg.Scripts
		}
	}
	if b, err := os.ReadFile(filepath.Join(path, "composer.json")); err == nil {
		d.compRaw = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(path, "go.mod")); err == nil {
		d.goMod = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(path, "Cargo.toml")); err == nil {
		d.cargo = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(path, "shard.yml")); err == nil {
		d.shard = string(b)
	}
	var py strings.Builder
	for _, f := range []string{"requirements.txt", "pyproject.toml", "Pipfile", "requirements/base.txt"} {
		if b, err := os.ReadFile(filepath.Join(path, f)); err == nil {
			py.Write(b)
			py.WriteByte('\n')
		}
	}
	d.pyDeps = strings.ToLower(py.String())
	return d
}

func (d *detector) has(rel ...string) bool {
	return fileExists(filepath.Join(append([]string{d.path}, rel...)...))
}

func (d *detector) hasAny(files ...string) bool {
	for _, f := range files {
		if d.has(f) {
			return true
		}
	}
	return false
}

func (d *detector) dep(name string) bool {
	_, ok := d.pkgDeps[name]
	return ok
}

func (d *detector) composerDep(name string) bool {
	return strings.Contains(d.compRaw, "\""+name+"\"")
}

func (d *detector) pyDep(name string) bool {
	return strings.Contains(d.pyDeps, name)
}

func (d *detector) hasScript(name string) bool {
	_, ok := d.pkgScr[name]
	return ok
}

// shardDep reports whether shard.yml lists a dependency (Crystal).
func (d *detector) shardDep(name string) bool {
	return regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(name) + `:\s*$`).MatchString(d.shard)
}

// crystalMain finds the file `crystal run` should start: the first target's
// `main:` in shard.yml, else src/<folder>.cr, else the first src/*.cr.
func (d *detector) crystalMain() string {
	if m := regexp.MustCompile(`(?m)^\s+main:\s*(\S+)`).FindStringSubmatch(d.shard); m != nil {
		return filepath.FromSlash(strings.Trim(m[1], `"'`))
	}
	base := filepath.Base(d.path)
	if cand := filepath.Join("src", base+".cr"); d.has(cand) {
		return cand
	}
	if matches, _ := filepath.Glob(filepath.Join(d.path, "src", "*.cr")); len(matches) > 0 {
		return filepath.Join("src", filepath.Base(matches[0]))
	}
	return filepath.Join("src", base+".cr")
}

// pythonEntry finds the module:app pair ASGI/WSGI servers need (main:app,
// app:app, src/main.py → src.main:app…).
func (d *detector) pythonEntry(attr string) string {
	for _, cand := range []string{"main", "app", "server", "api", "src/main", "src/app", "app/main"} {
		if d.has(cand + ".py") {
			return strings.ReplaceAll(cand, "/", ".") + ":" + attr
		}
	}
	return "main:" + attr
}

// ---- command helpers ----

func npx(args ...string) func(*detector, int) (string, []string) {
	return func(d *detector, port int) (string, []string) {
		out := make([]string, len(args))
		for i, a := range args {
			out[i] = strings.ReplaceAll(a, "{port}", strconv.Itoa(port))
		}
		return "npx", out
	}
}

// npmScript runs the project's own "dev" (or "start") script; PORT/HOST are
// exported by StartDevServer so most servers pick the assigned port up.
func npmScript(d *detector, port int) (string, []string) {
	if d.hasScript("dev") {
		return "npm", []string{"run", "dev"}
	}
	if d.hasScript("start") {
		return "npm", []string{"start"}
	}
	return "node", []string{"."}
}

func uvicorn(d *detector, port int) (string, []string) {
	return "python", []string{"-m", "uvicorn", d.pythonEntry("app"), "--reload", "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
}

func flask(d *detector, port int) (string, []string) {
	return "python", []string{"-m", "flask", "run", "--debug", "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
}

func goRun(*detector, int) (string, []string)    { return "go", []string{"run", "."} }
func cargoRun(*detector, int) (string, []string) { return "cargo", []string{"run"} }

// kemalRun starts a Kemal app; Kemal.run parses --bind/--port from ARGV.
func kemalRun(d *detector, port int) (string, []string) {
	return "crystal", []string{"run", d.crystalMain(), "--", "--bind", "127.0.0.1", "--port", strconv.Itoa(port)}
}

func crystalRun(d *detector, _ int) (string, []string) {
	return "crystal", []string{"run", d.crystalMain()}
}

// Catalog is the ordered framework registry. Order matters for detection.
var Catalog = []Framework{
	// ---- PHP (served by a web server) ----
	{Name: "Laravel", Runtime: "php", detect: func(d *detector) bool {
		return d.has("artisan") && d.composerDep("laravel/framework")
	}},
	{Name: "Lumen", Runtime: "php", detect: func(d *detector) bool {
		return d.has("artisan") && d.composerDep("laravel/lumen-framework")
	}},
	{Name: "WordPress", Runtime: "php", detect: func(d *detector) bool {
		return d.hasAny("wp-config.php", "wp-config-sample.php") || d.has("wp-includes", "version.php")
	}},
	{Name: "Symfony", Runtime: "php", detect: func(d *detector) bool {
		return d.has("symfony.lock") || (d.has("bin", "console") && d.has("config", "bundles.php"))
	}},
	{Name: "CodeIgniter", Runtime: "php", detect: func(d *detector) bool {
		return d.has("spark") || d.has("system", "core", "CodeIgniter.php") || d.composerDep("codeigniter4/framework")
	}},
	{Name: "Yii", Runtime: "php", detect: func(d *detector) bool {
		return (d.has("yii") && d.has("composer.json")) || d.composerDep("yiisoft/yii2")
	}},
	{Name: "CakePHP", Runtime: "php", detect: func(d *detector) bool {
		return d.has("bin", "cake") || d.composerDep("cakephp/cakephp")
	}},
	{Name: "Drupal", Runtime: "php", detect: func(d *detector) bool {
		return d.has("web", "core", "lib", "Drupal.php") || d.has("core", "lib", "Drupal.php") || d.composerDep("drupal/core-recommended")
	}},
	{Name: "Slim", Runtime: "php", detect: func(d *detector) bool { return d.composerDep("slim/slim") }},
	{Name: "Laminas", Runtime: "php", detect: func(d *detector) bool {
		return d.composerDep("laminas/laminas-mvc") || d.composerDep("mezzio/mezzio")
	}},
	{Name: "Joomla", Runtime: "php", detect: func(d *detector) bool {
		return d.has("configuration.php") && d.has("administrator", "index.php")
	}},
	{Name: "Magento", Runtime: "php", detect: func(d *detector) bool { return d.composerDep("magento/product-community-edition") }},
	{Name: "PrestaShop", Runtime: "php", detect: func(d *detector) bool {
		return d.has("config", "settings.inc.php") || d.composerDep("prestashop/prestashop")
	}},
	{Name: "PHP", Runtime: "php", detect: func(d *detector) bool {
		return d.has("composer.json") || d.has("index.php") || d.has("public", "index.php")
	}},

	// ---- Node (own dev server) ----
	{Name: "Next.js", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool {
			return d.dep("next") || d.hasAny("next.config.js", "next.config.mjs", "next.config.ts")
		},
		start: npx("next", "dev", "-p", "{port}")},
	{Name: "Nuxt", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("nuxt") || d.hasAny("nuxt.config.js", "nuxt.config.ts") },
		start:  npx("nuxt", "dev", "--port", "{port}")},
	{Name: "NestJS", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("@nestjs/core") || d.has("nest-cli.json") },
		start:  npx("nest", "start", "--watch")},
	{Name: "Astro", Runtime: "node", AppServer: true, Port: 4321,
		detect: func(d *detector) bool {
			return d.dep("astro") || d.hasAny("astro.config.mjs", "astro.config.ts", "astro.config.js")
		},
		start: npx("astro", "dev", "--port", "{port}", "--host", "127.0.0.1")},
	{Name: "Remix", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool { return d.dep("@remix-run/react") || d.dep("@remix-run/dev") },
		start:  npx("remix", "vite:dev", "--port", "{port}")},
	{Name: "SvelteKit", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool { return d.dep("@sveltejs/kit") },
		start:  npx("vite", "dev", "--port", "{port}", "--strictPort")},
	{Name: "Angular", Runtime: "node", AppServer: true, Port: 4200,
		detect: func(d *detector) bool { return d.dep("@angular/core") || d.has("angular.json") },
		start:  npx("ng", "serve", "--port", "{port}")},
	{Name: "Gatsby", Runtime: "node", AppServer: true, Port: 8000,
		detect: func(d *detector) bool { return d.dep("gatsby") },
		start:  npx("gatsby", "develop", "-p", "{port}")},
	{Name: "Vue", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool { return d.dep("vue") && (d.dep("vite") || d.has("vue.config.js")) },
		start:  npx("vite", "--port", "{port}", "--strictPort")},
	{Name: "Svelte", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool { return d.dep("svelte") },
		start:  npx("vite", "--port", "{port}", "--strictPort")},
	{Name: "React", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool { return d.dep("react") && !d.dep("react-native") },
		start: func(d *detector, port int) (string, []string) {
			if d.dep("react-scripts") { // Create React App reads PORT from the env
				return "npx", []string{"react-scripts", "start"}
			}
			return "npx", []string{"vite", "--port", strconv.Itoa(port), "--strictPort"}
		}},
	{Name: "AdonisJS", Runtime: "node", AppServer: true, Port: 3333,
		detect: func(d *detector) bool { return d.dep("@adonisjs/core") },
		start:  func(d *detector, port int) (string, []string) { return "node", []string{"ace", "serve", "--watch"} }},
	{Name: "Express", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("express") }, start: npmScript},
	{Name: "Fastify", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("fastify") }, start: npmScript},
	{Name: "Koa", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("koa") }, start: npmScript},
	{Name: "Hono", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.dep("hono") }, start: npmScript},
	{Name: "Vite", Runtime: "node", AppServer: true, Port: 5173,
		detect: func(d *detector) bool {
			return d.dep("vite") || d.hasAny("vite.config.js", "vite.config.ts", "vite.config.mjs")
		},
		start: npx("vite", "--port", "{port}", "--strictPort")},
	{Name: "Node", Runtime: "node", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.has("package.json") }, start: npmScript},

	// ---- Python ----
	{Name: "Django", Runtime: "python", AppServer: true, Port: 8000,
		detect: func(d *detector) bool { return d.has("manage.py") },
		start: func(d *detector, port int) (string, []string) {
			return "python", []string{"manage.py", "runserver", fmt.Sprintf("127.0.0.1:%d", port)}
		}},
	{Name: "FastAPI", Runtime: "python", AppServer: true, Port: 8000,
		detect: func(d *detector) bool { return d.pyDep("fastapi") }, start: uvicorn},
	{Name: "Flask", Runtime: "python", AppServer: true, Port: 5000,
		detect: func(d *detector) bool { return d.pyDep("flask") }, start: flask},
	{Name: "Python", Runtime: "python", AppServer: true, Port: 8000,
		detect: func(d *detector) bool {
			return d.hasAny("requirements.txt", "pyproject.toml", "Pipfile", "main.py", "app.py")
		},
		start: func(d *detector, port int) (string, []string) {
			for _, f := range []string{"main.py", "app.py", "server.py"} {
				if d.has(f) {
					return "python", []string{f}
				}
			}
			return "python", []string{"-m", "http.server", strconv.Itoa(port)}
		}},

	// ---- Go ----
	{Name: "Goravel", Runtime: "go", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return strings.Contains(d.goMod, "github.com/goravel/framework") }, start: goRun},
	{Name: "Gin", Runtime: "go", AppServer: true, Port: 8080,
		detect: func(d *detector) bool { return strings.Contains(d.goMod, "github.com/gin-gonic/gin") }, start: goRun},
	{Name: "Fiber", Runtime: "go", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return strings.Contains(d.goMod, "github.com/gofiber/fiber") }, start: goRun},
	{Name: "Echo", Runtime: "go", AppServer: true, Port: 8080,
		detect: func(d *detector) bool { return strings.Contains(d.goMod, "github.com/labstack/echo") }, start: goRun},
	{Name: "Go", Runtime: "go", AppServer: true, Port: 8080,
		detect: func(d *detector) bool { return d.has("go.mod") }, start: goRun},

	// ---- Rust ----
	{Name: "Actix", Runtime: "rust", AppServer: true, Port: 8080,
		detect: func(d *detector) bool { return strings.Contains(d.cargo, "actix-web") }, start: cargoRun},
	{Name: "Axum", Runtime: "rust", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return strings.Contains(d.cargo, "axum") }, start: cargoRun},
	{Name: "Rocket", Runtime: "rust", AppServer: true, Port: 8000,
		detect: func(d *detector) bool { return strings.Contains(d.cargo, "rocket") }, start: cargoRun},
	// ---- Crystal (plugin runtime; needs the "crystal" vfox plugin) ----
	{Name: "Kemal", Runtime: "crystal", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.shardDep("kemal") }, start: kemalRun},
	{Name: "Crystal", Runtime: "crystal", AppServer: true, Port: 3000,
		detect: func(d *detector) bool { return d.has("shard.yml") }, start: crystalRun},

	{Name: "Rust", Runtime: "rust", AppServer: true, Port: 8080,
		detect: func(d *detector) bool { return d.has("Cargo.toml") }, start: cargoRun},

	// ---- Static ----
	{Name: "Static", Runtime: "static", detect: func(d *detector) bool {
		return d.hasAny("index.html", "index.htm") || d.has("public", "index.html")
	}},
}

// catalogByName indexes the catalog for the lookups below.
var catalogByName = func() map[string]*Framework {
	m := make(map[string]*Framework, len(Catalog))
	for i := range Catalog {
		m[Catalog[i].Name] = &Catalog[i]
	}
	return m
}()

// LookupFramework returns the catalog entry for a framework name, or nil.
func LookupFramework(name string) *Framework {
	return catalogByName[name]
}

// DetectFramework probes a folder and returns the first matching catalog entry
// name ("" when nothing is recognised).
func DetectFramework(projectPath string) string {
	d := newDetector(projectPath)
	for i := range Catalog {
		if Catalog[i].detect != nil && Catalog[i].detect(d) {
			return Catalog[i].Name
		}
	}
	return ""
}

// RuntimeFromFramework maps a framework name to the underlying runtime.
// Returns "" for unrecognised frameworks.
func RuntimeFromFramework(framework string) string {
	if f := LookupFramework(framework); f != nil {
		return f.Runtime
	}
	return ""
}

// IsAppServer returns true if the framework runs its own HTTP server (Node,
// Python, Go, Rust…). Unknown non-empty names are treated as app servers so a
// project imported from a future DevBox keeps its Start button.
func IsAppServer(framework string) bool {
	if f := LookupFramework(framework); f != nil {
		return f.AppServer
	}
	return framework != ""
}

// DefaultPort returns the preferred dev-server port for a framework.
func DefaultPort(framework string) int {
	if f := LookupFramework(framework); f != nil {
		return f.Port
	}
	return 0
}

// GetStartCommand returns the command and args to start a dev server for a
// framework inside projectPath. The port is substituted where the CLI accepts
// one; otherwise the PORT env var exported by StartDevServer applies.
func GetStartCommand(framework, projectPath string, port int) (string, []string) {
	f := LookupFramework(framework)
	if f == nil || !f.AppServer {
		return "", nil
	}
	d := newDetector(projectPath)
	if f.start != nil {
		return f.start(d, port)
	}
	switch f.Runtime {
	case "node":
		return npmScript(d, port)
	case "go":
		return goRun(d, port)
	case "rust":
		return cargoRun(d, port)
	case "python":
		return "python", []string{"-m", "http.server", strconv.Itoa(port)}
	}
	return "", nil
}
