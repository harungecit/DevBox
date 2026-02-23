package project

import (
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
// httpPort is the Apache listen port. If 0, defaults to 80.
func GenerateApacheVhost(project Project, httpPort int) error {
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
		conf = fmt.Sprintf(`# DevBox generated vhost for %s
<VirtualHost *:%d>
    ServerName %s
    DocumentRoot "%s"

    <Directory "%s">
        Options Indexes FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>
</VirtualHost>
`, project.Name, httpPort, project.Domain, docRoot, docRoot)
	}

	return os.WriteFile(confPath, []byte(conf), 0644)
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
