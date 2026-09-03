package modules

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"DevBox/internal/vfox/codec"

	lua "github.com/yuin/gopher-lua"
)

// preloadHTTP registers `require("http")` with vfox's API:
//
//	local resp, err = http.get({ url = "...", headers = {...} })
//	-- resp = { body, status_code, headers, content_length }
//	local resp, err = http.head({ url = "..." })
//	local err = http.download_file({ url = "..." }, "/path/to/file")
//
// Requests carry the plugin's User-Agent (VFOX_NAVIGATOR) unless the plugin
// sets one, and are bound to the Lua context so hook timeouts abort them.
func preloadHTTP(L *lua.LState, h *Host) {
	L.PreloadModule("http", func(L *lua.LState) int {
		t := L.NewTable()
		L.SetFuncs(t, map[string]lua.LGFunction{
			"get":           func(L *lua.LState) int { return h.httpGet(L, "GET") },
			"head":          func(L *lua.LState) int { return h.httpGet(L, "HEAD") },
			"download_file": h.httpDownloadFile,
		})
		L.Push(t)
		return 1
	})
}

func (h *Host) buildRequest(L *lua.LState, method string, param *lua.LTable) (*http.Request, string) {
	urlStr := param.RawGetString("url")
	if urlStr == lua.LNil {
		return nil, "url is required"
	}
	req, err := http.NewRequest(method, urlStr.String(), nil)
	if err != nil {
		return nil, err.Error()
	}
	if ctx := L.Context(); ctx != nil {
		req = req.WithContext(ctx)
	}
	if headers, ok := param.RawGetString("headers").(*lua.LTable); ok {
		headers.ForEach(func(key lua.LValue, value lua.LValue) {
			req.Header.Add(key.String(), value.String())
		})
	}
	if req.Header.Get("User-Agent") == "" {
		if nav := L.GetGlobal(codec.NavigatorObjKey); nav != lua.LNil {
			var navigator codec.Navigator
			if codec.Unmarshal(nav, &navigator) == nil && navigator.UserAgent != "" {
				req.Header.Set("User-Agent", navigator.UserAgent)
			}
		}
	}
	return req, ""
}

func (h *Host) httpGet(L *lua.LState, method string) int {
	param := L.CheckTable(1)
	req, msg := h.buildRequest(L, method, param)
	if req == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(msg))
		return 2
	}
	resp, err := h.client().Do(req)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer resp.Body.Close()

	headers := L.NewTable()
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers.RawSetString(k, lua.LString(v[0]))
		}
	}
	result := L.NewTable()
	if method != "HEAD" {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.SetField(result, "body", lua.LString(body))
	}
	L.SetField(result, "status_code", lua.LNumber(resp.StatusCode))
	L.SetField(result, "headers", headers)
	L.SetField(result, "content_length", lua.LNumber(resp.ContentLength))
	L.Push(result)
	return 1
}

func (h *Host) httpDownloadFile(L *lua.LState) int {
	param := L.CheckTable(1)
	fp := L.CheckString(2)
	if fp == "" {
		L.Push(lua.LString("filepath is required"))
		return 1
	}
	req, msg := h.buildRequest(L, "GET", param)
	if req == nil {
		L.Push(lua.LString(msg))
		return 1
	}
	resp, err := h.client().Do(req)
	if err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		L.Push(lua.LString("file not found"))
		return 1
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		L.Push(lua.LString("HTTP " + resp.Status))
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	out, err := os.Create(fp)
	if err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		L.Push(lua.LString(err.Error()))
		return 1
	}
	return 0
}
