/*
 *    Copyright 2026 Han Li and contributors
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 */

package modules

import (
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// preloadStrings registers the Go-strings helpers vfox exposes as
// `require("vfox.strings")` (and `require("strings")` for convenience).
func preloadStrings(L *lua.LState) {
	L.PreloadModule("vfox.strings", stringsLoader)
	L.PreloadModule("strings", stringsLoader)
}

func stringsLoader(L *lua.LState) int {
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"split":       strSplit,
		"trim":        strTrim,
		"trim_space":  strTrimSpace,
		"trim_prefix": strTrimPrefix,
		"trim_suffix": strTrimSuffix,
		"has_prefix":  strHasPrefix,
		"has_suffix":  strHasSuffix,
		"contains":    strContains,
		"fields":      strFields,
		"join":        strJoin,
	})
	L.Push(t)
	return 1
}

func strJoin(L *lua.LState) int {
	tbl := L.CheckTable(1)
	sep := L.CheckString(2)
	var arr []string
	tbl.ForEach(func(_, value lua.LValue) {
		arr = append(arr, value.String())
	})
	L.Push(lua.LString(strings.Join(arr, sep)))
	return 1
}

func strSplit(L *lua.LState) int {
	str := L.CheckString(1)
	deli := ""
	if L.GetTop() > 1 {
		deli = L.CheckString(2)
	}
	parts := strings.Split(str, deli)
	result := L.CreateTable(len(parts), 0)
	for _, p := range parts {
		result.Append(lua.LString(p))
	}
	L.Push(result)
	return 1
}

func strFields(L *lua.LState) int {
	parts := strings.Fields(L.CheckString(1))
	result := L.CreateTable(len(parts), 0)
	for _, p := range parts {
		result.Append(lua.LString(p))
	}
	L.Push(result)
	return 1
}

func strHasPrefix(L *lua.LState) int {
	L.Push(lua.LBool(strings.HasPrefix(L.CheckString(1), L.CheckString(2))))
	return 1
}

func strHasSuffix(L *lua.LState) int {
	L.Push(lua.LBool(strings.HasSuffix(L.CheckString(1), L.CheckString(2))))
	return 1
}

func strTrim(L *lua.LState) int {
	L.Push(lua.LString(strings.Trim(L.CheckString(1), L.CheckString(2))))
	return 1
}

func strTrimSpace(L *lua.LState) int {
	L.Push(lua.LString(strings.TrimSpace(L.CheckString(1))))
	return 1
}

func strTrimPrefix(L *lua.LState) int {
	L.Push(lua.LString(strings.TrimPrefix(L.CheckString(1), L.CheckString(2))))
	return 1
}

func strTrimSuffix(L *lua.LState) int {
	L.Push(lua.LString(strings.TrimSuffix(L.CheckString(1), L.CheckString(2))))
	return 1
}

func strContains(L *lua.LState) int {
	L.Push(lua.LBool(strings.Contains(L.CheckString(1), L.CheckString(2))))
	return 1
}
