package project

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectFramework(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"laravel", map[string]string{"artisan": "", "composer.json": `{"require":{"laravel/framework":"^11"}}`}, "Laravel"},
		{"symfony", map[string]string{"bin/console": "", "config/bundles.php": ""}, "Symfony"},
		{"codeigniter4", map[string]string{"spark": "", "composer.json": "{}"}, "CodeIgniter"},
		{"slim", map[string]string{"composer.json": `{"require":{"slim/slim":"^4"}}`}, "Slim"},
		{"cake", map[string]string{"bin/cake": ""}, "CakePHP"},
		{"yii", map[string]string{"yii": "", "composer.json": `{"require":{"yiisoft/yii2":"~2"}}`}, "Yii"},
		{"wordpress", map[string]string{"wp-config-sample.php": ""}, "WordPress"},
		{"plain php", map[string]string{"index.php": "<?php"}, "PHP"},
		{"nextjs", map[string]string{"package.json": `{"dependencies":{"next":"15","react":"19"}}`}, "Next.js"},
		{"nuxt", map[string]string{"package.json": `{"dependencies":{"nuxt":"3","vue":"3"}}`}, "Nuxt"},
		{"nest", map[string]string{"package.json": `{"dependencies":{"@nestjs/core":"10"}}`}, "NestJS"},
		{"astro", map[string]string{"package.json": `{"dependencies":{"astro":"4"}}`}, "Astro"},
		{"sveltekit", map[string]string{"package.json": `{"devDependencies":{"@sveltejs/kit":"2","svelte":"5","vite":"5"}}`}, "SvelteKit"},
		{"angular", map[string]string{"angular.json": "{}", "package.json": `{"dependencies":{"@angular/core":"18"}}`}, "Angular"},
		{"react vite", map[string]string{"package.json": `{"dependencies":{"react":"19"},"devDependencies":{"vite":"5"}}`}, "React"},
		{"express", map[string]string{"package.json": `{"dependencies":{"express":"4"},"scripts":{"dev":"nodemon"}}`}, "Express"},
		{"generic node", map[string]string{"package.json": `{"name":"x","scripts":{"start":"node index.js"}}`}, "Node"},
		{"django", map[string]string{"manage.py": ""}, "Django"},
		{"fastapi", map[string]string{"requirements.txt": "fastapi\nuvicorn", "main.py": ""}, "FastAPI"},
		{"flask", map[string]string{"pyproject.toml": "[project]\ndependencies=['Flask']"}, "Flask"},
		{"goravel", map[string]string{"go.mod": "module x\nrequire github.com/goravel/framework v1.15.0"}, "Goravel"},
		{"gin", map[string]string{"go.mod": "module x\nrequire github.com/gin-gonic/gin v1.10.0"}, "Gin"},
		{"plain go", map[string]string{"go.mod": "module x"}, "Go"},
		{"axum", map[string]string{"Cargo.toml": "[dependencies]\naxum = \"0.7\""}, "Axum"},
		{"kemal", map[string]string{"shard.yml": "name: blog\ndependencies:\n  kemal:\n    github: kemalcr/kemal\n"}, "Kemal"},
		{"plain crystal", map[string]string{"shard.yml": "name: tool\nversion: 0.1.0\n"}, "Crystal"},
		{"static", map[string]string{"index.html": "<h1>hi</h1>"}, "Static"},
		{"empty", map[string]string{"README.md": ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeFiles(t, c.files)
			if got := DetectFramework(dir); got != c.want {
				t.Fatalf("DetectFramework = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCatalogConsistency(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Catalog {
		if seen[f.Name] {
			t.Errorf("duplicate framework %q", f.Name)
		}
		seen[f.Name] = true
		if f.detect == nil {
			t.Errorf("%s has no detector", f.Name)
		}
		if f.AppServer && f.Port == 0 {
			t.Errorf("%s is an app server without a default port", f.Name)
		}
		if !f.AppServer && f.start != nil {
			t.Errorf("%s is web-served but has a start command", f.Name)
		}
		if f.AppServer {
			exe, _ := GetStartCommand(f.Name, t.TempDir(), 3000)
			if exe == "" {
				t.Errorf("%s: no start command", f.Name)
			}
		}
	}
}

func TestStartCommandsUsePort(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.py": "", "requirements.txt": "fastapi"})
	exe, args := GetStartCommand("FastAPI", dir, 8123)
	if exe != "python" || args[2] != "main:app" || args[len(args)-1] != "8123" {
		t.Fatalf("unexpected FastAPI command: %s %v", exe, args)
	}
	exe, args = GetStartCommand("Next.js", dir, 3005)
	if exe != "npx" || args[len(args)-1] != "3005" {
		t.Fatalf("unexpected Next.js command: %s %v", exe, args)
	}
}
