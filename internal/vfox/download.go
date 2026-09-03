package vfox

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"DevBox/internal/vfox/archive"
	"DevBox/internal/vfox/modules"
)

// FileNameFromURL picks the on-disk name for a download the way vfox does:
// the URL path's base name, overridden by a "#name.ext" fragment; when that
// carries no extension the redirected URL and Content-Disposition are
// consulted, because the name decides which decompressor runs.
func FileNameFromURL(rawURL string, resp *http.Response) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	name := path.Base(u.Path)
	if u.Fragment != "" {
		name = path.Base(u.Fragment)
	}
	if resp != nil && !archive.IsKnownArchive(name) {
		if cd := resp.Header.Get("Content-Disposition"); cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil {
				if fn := path.Base(params["filename"]); fn != "" && fn != "." && fn != "/" {
					name = fn
				}
			}
		}
		if !archive.IsKnownArchive(name) && resp.Request != nil && resp.Request.URL != nil {
			if fn := path.Base(resp.Request.URL.Path); archive.IsKnownArchive(fn) {
				name = fn
			}
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "download"
	}
	return name
}

// Download fetches rawURL into destDir with the plugin's headers and
// User-Agent, reporting (done, total) bytes; total is -1 when unknown.
// It returns the full path of the written file.
func Download(ctx context.Context, rawURL, destDir string, headers map[string]string, ua string, onProgress func(done, total int64)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" && ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := modules.DefaultHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, rawURL)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, FileNameFromURL(rawURL, resp))
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	var lastReport int64 = -1
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(dest)
				return "", werr
			}
			done += int64(n)
			if onProgress != nil && (done-lastReport > 256*1024 || readErr == io.EOF) {
				lastReport = done
				onProgress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(dest)
			return "", fmt.Errorf("download interrupted: %w", readErr)
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if onProgress != nil {
		onProgress(done, total)
	}
	return dest, nil
}
