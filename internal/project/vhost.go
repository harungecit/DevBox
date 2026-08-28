package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"DevBox/internal/config"
)

// nginxLocationBlock returns the appropriate location block based on project type.
// For app-server projects (Node, Go, Python, etc.) it returns a reverse proxy block.
// For PHP/Static projects it returns file serving + optional PHP FastCGI.
func nginxLocationBlock(project Project, docRoot string, phpCgiPort int) string {
	if IsAppServer(project.Framework) && project.Port > 0 {
		// Reverse proxy to app's dev server
		return fmt.Sprintf(`
    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }`, project.Port)
	}

	// File serving for PHP/Static projects
	phpBlock := ""
	if phpCgiPort > 0 {
		phpBlock = fmt.Sprintf(`

    location ~ \.php$ {
        fastcgi_pass 127.0.0.1:%d;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }`, phpCgiPort)
	}

	return fmt.Sprintf(`
    root %s;
    index index.php index.html index.htm;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }%s

    location ~ /\.ht {
        deny all;
    }`, docRoot, phpBlock)
}

// GenerateNginxVhost creates an nginx vhost config for a project.
// httpPort is the nginx listen port (e.g. 81). If 0, defaults to 80.
func GenerateNginxVhost(project Project, phpCgiPort int, httpPort int) error {
	base := filepath.Join(config.GetDataDir(), "services", "nginx")
	vhostDir := filepath.Join(base, "conf", "vhosts")
	os.MkdirAll(vhostDir, 0755)

	confPath := filepath.Join(vhostDir, project.Name+".conf")
	docRoot := strings.ReplaceAll(project.Path, "\\", "/")

	// Determine if there's a public/ subdirectory (Laravel, Symfony, etc.)
	publicDir := filepath.Join(project.Path, "public")
	if _, err := os.Stat(publicDir); err == nil {
		docRoot = strings.ReplaceAll(publicDir, "\\", "/")
	}

	if httpPort <= 0 {
		httpPort = 80
	}

	locationBlock := nginxLocationBlock(project, docRoot, phpCgiPort)

	if project.SSL {
		certDir := filepath.Join(config.GetDataDir(), "ssl", "certs")
		certFile := strings.ReplaceAll(filepath.Join(certDir, project.Domain+".pem"), "\\", "/")
		keyFile := strings.ReplaceAll(filepath.Join(certDir, project.Domain+"-key.pem"), "\\", "/")

		conf := fmt.Sprintf(`# DevBox generated vhost for %s
# HTTP -> HTTPS redirect
server {
    listen %d;
    server_name %s;
    return 301 https://$host$request_uri;
}

# HTTPS server
server {
    listen 443 ssl;
    server_name %s;
    ssl_certificate %s;
    ssl_certificate_key %s;
%s
}
`, project.Name, httpPort, project.Domain, project.Domain, certFile, keyFile, locationBlock)

		return os.WriteFile(confPath, []byte(conf), 0644)
	}

	// Non-SSL vhost
	conf := fmt.Sprintf(`# DevBox generated vhost for %s
server {
    listen %d;
    server_name %s;
%s
}
`, project.Name, httpPort, project.Domain, locationBlock)

	return os.WriteFile(confPath, []byte(conf), 0644)
}

// GenerateApacheVhost creates an Apache vhost config for a project.
// httpPort is the Apache listen port. If 0, defaults to 80. phpCgiPort > 0
// routes *.php through mod_proxy_fcgi to that php-cgi instance.
func GenerateApacheVhost(project Project, httpPort int, phpCgiPort int) error {
	base := filepath.Join(config.GetDataDir(), "services", "apache")
	vhostDir := filepath.Join(base, "conf", "extra")
	os.MkdirAll(vhostDir, 0755)

	if httpPort <= 0 {
		httpPort = 80
	}

	confPath := filepath.Join(vhostDir, "vhost-"+project.Name+".conf")
	docRoot := strings.ReplaceAll(project.Path, "\\", "/")

	publicDir := filepath.Join(project.Path, "public")
	if _, err := os.Stat(publicDir); err == nil {
		docRoot = strings.ReplaceAll(publicDir, "\\", "/")
	}

	var conf string
	if IsAppServer(project.Framework) && project.Port > 0 {
		conf = fmt.Sprintf(`# DevBox generated vhost for %s
<VirtualHost *:%d>
    ServerName %s
    ProxyPreserveHost On
    ProxyPass / http://127.0.0.1:%d/
    ProxyPassReverse / http://127.0.0.1:%d/
</VirtualHost>
`, project.Name, httpPort, project.Domain, project.Port, project.Port)
	} else {
		phpBlock := ""
		if phpCgiPort > 0 {
			phpBlock = fmt.Sprintf(`
    <FilesMatch "\.php$">
        SetHandler "proxy:fcgi://127.0.0.1:%d"
    </FilesMatch>
    DirectoryIndex index.php index.html
`, phpCgiPort)
		}
		conf = fmt.Sprintf(`# DevBox generated vhost for %s
<VirtualHost *:%d>
    ServerName %s
    DocumentRoot "%s"
%s
    <Directory "%s">
        Options Indexes FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>
</VirtualHost>
`, project.Name, httpPort, project.Domain, docRoot, phpBlock, docRoot)
	}

	return os.WriteFile(confPath, []byte(conf), 0644)
}

// RemoveApacheVhost removes a project's Apache vhost config.
func RemoveApacheVhost(projectName string) {
	base := filepath.Join(config.GetDataDir(), "services", "apache")
	os.Remove(filepath.Join(base, "conf", "extra", "vhost-"+projectName+".conf"))
}

// GenerateCaddyVhost creates a Caddy site block for a project
func GenerateCaddyVhost(project Project, phpCgiPort int) error {
	base := filepath.Join(config.GetDataDir(), "services", "caddy")
	docRoot := strings.ReplaceAll(project.Path, "\\", "/")

	publicDir := filepath.Join(project.Path, "public")
	if _, err := os.Stat(publicDir); err == nil {
		docRoot = strings.ReplaceAll(publicDir, "\\", "/")
	}

	var conf string
	if IsAppServer(project.Framework) && project.Port > 0 {
		conf = fmt.Sprintf(`# DevBox generated site for %s
%s {
    reverse_proxy 127.0.0.1:%d
}
`, project.Name, project.Domain, project.Port)
	} else {
		phpBlock := ""
		if phpCgiPort > 0 {
			phpBlock = fmt.Sprintf("\n    php_fastcgi 127.0.0.1:%d", phpCgiPort)
		}
		conf = fmt.Sprintf(`# DevBox generated site for %s
%s {
    root * %s
    file_server%s
    encode gzip
}
`, project.Name, project.Domain, docRoot, phpBlock)
	}

	// Append to Caddyfile
	caddyfile := filepath.Join(base, "Caddyfile")
	f, err := os.OpenFile(caddyfile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("\n" + conf)
	return err
}

// RemoveNginxVhost removes an nginx vhost config
func RemoveNginxVhost(projectName string) {
	base := filepath.Join(config.GetDataDir(), "services", "nginx")
	os.Remove(filepath.Join(base, "conf", "vhosts", projectName+".conf"))
}

// GenerateFrankenPHPVhost writes a per-project Caddyfile fragment for FrankenPHP.
// Files land in ~/.devbox/services/frankenphp/vhosts/<name>.caddy and are
// picked up by the main Caddyfile via `import vhosts/*.caddy`.
//
// FrankenPHP serves PHP using its bundled runtime via the `php_server`
// directive — no external php-fpm/php-cgi is needed. App-server projects
// (Node/Go/etc.) get a reverse_proxy block, the same shape we use for Caddy.
func GenerateFrankenPHPVhost(p Project) error {
	if p.Domain == "" {
		return nil
	}
	base := filepath.Join(config.GetDataDir(), "services", "frankenphp")
	vhostDir := filepath.Join(base, "vhosts")
	if err := os.MkdirAll(vhostDir, 0755); err != nil {
		return err
	}

	docRoot := strings.ReplaceAll(p.Path, "\\", "/")
	publicDir := filepath.Join(p.Path, "public")
	if _, err := os.Stat(publicDir); err == nil {
		docRoot = strings.ReplaceAll(publicDir, "\\", "/")
	}

	listen := fmt.Sprintf(":%d", frankenphpListenPort())
	addr := fmt.Sprintf("http://%s%s", p.Domain, listen)

	var conf string
	if IsAppServer(p.Framework) && p.Port > 0 {
		conf = fmt.Sprintf(`# DevBox FrankenPHP vhost for %s (reverse proxy to dev server)
%s {
	reverse_proxy 127.0.0.1:%d
}
`, p.Name, addr, p.Port)
	} else {
		// PHP / static: serve files; php_server enables PHP via FrankenPHP's bundled runtime.
		conf = fmt.Sprintf(`# DevBox FrankenPHP vhost for %s
%s {
	root * %s
	encode gzip
	php_server
}
`, p.Name, addr, docRoot)
	}

	confPath := filepath.Join(vhostDir, p.Name+".caddy")
	return os.WriteFile(confPath, []byte(conf), 0644)
}

// RemoveFrankenPHPVhost removes a project's FrankenPHP vhost fragment.
func RemoveFrankenPHPVhost(projectName string) {
	base := filepath.Join(config.GetDataDir(), "services", "frankenphp")
	os.Remove(filepath.Join(base, "vhosts", projectName+".caddy"))
}

// frankenphpListenPort reads the FrankenPHP service's current port from its
// saved config. Defaults to 8501 if anything is missing — matches the
// service manager's DefaultPort and keeps vhosts consistent with the bind.
func frankenphpListenPort() int {
	// Minimal read of services/frankenphp/devbox-service.json without importing
	// the service package to avoid a cycle.
	type svcCfg struct {
		Port int `json:"port"`
	}
	cfgPath := filepath.Join(config.GetDataDir(), "services", "frankenphp", "devbox-service.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return 8501
	}
	var c svcCfg
	if json.Unmarshal(data, &c) != nil || c.Port <= 0 {
		return 8501
	}
	return c.Port
}
