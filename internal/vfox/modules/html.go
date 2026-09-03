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

	"github.com/PuerkitoBio/goquery"
	lua "github.com/yuin/gopher-lua"
)

const luaHTMLDocumentTypeName = "html_document"
const luaSelectionTypeName = "html_selection"

// preloadHTML registers `require("html")`: html.parse(str) → document with
// :find(selector); selections support text/html/find/first/last/each/attr/eq.
func preloadHTML(L *lua.LState) {
	L.PreloadModule("html", htmlLoader)
}

func htmlLoader(L *lua.LState) int {
	docMt := L.NewTypeMetatable(luaHTMLDocumentTypeName)
	L.SetField(docMt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"find": documentFind,
	}))
	selectionMt := L.NewTypeMetatable(luaSelectionTypeName)
	L.SetField(selectionMt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"text":  selectionText,
		"html":  selectionHTML,
		"find":  selectionFind,
		"first": selectionFirst,
		"last":  selectionLast,
		"each":  selectionEach,
		"attr":  selectionAttr,
		"eq":    selectionEq,
	}))
	table := L.NewTable()
	L.SetField(table, "parse", L.NewFunction(newHTMLDocument))
	L.Push(table)
	return 1
}

func pushSelection(L *lua.LState, s *goquery.Selection) int {
	ud := L.NewUserData()
	ud.Value = s
	L.SetMetatable(ud, L.GetTypeMetatable(luaSelectionTypeName))
	L.Push(ud)
	return 1
}

func selectionEq(L *lua.LState) int {
	s := checkSelection(L)
	idx := L.CheckInt(2)
	return pushSelection(L, s.Eq(idx))
}

func selectionAttr(L *lua.LState) int {
	s := checkSelection(L)
	attr := L.CheckString(2)
	ret, ok := s.Attr(attr)
	if !ok {
		L.Push(lua.LNil)
		return 1
	}
	L.Push(lua.LString(ret))
	return 1
}

func selectionEach(L *lua.LState) int {
	s := checkSelection(L)
	f := L.CheckFunction(2)
	s.Each(func(i int, selection *goquery.Selection) {
		ud := L.NewUserData()
		ud.Value = selection
		L.SetMetatable(ud, L.GetTypeMetatable(luaSelectionTypeName))
		err := L.CallByParam(lua.P{
			Fn:      f,
			NRet:    0,
			Protect: true,
		}, lua.LNumber(i+1), ud)
		if err != nil {
			L.RaiseError("%s", err.Error())
		}
	})
	return 0
}

func newHTMLDocument(L *lua.LState) int {
	src := L.CheckString(1)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(src))
	if err != nil {
		L.RaiseError("%s", err.Error())
		return 0
	}
	ud := L.NewUserData()
	ud.Value = doc
	L.SetMetatable(ud, L.GetTypeMetatable(luaHTMLDocumentTypeName))
	L.Push(ud)
	return 1
}

func selectionFirst(L *lua.LState) int { return pushSelection(L, checkSelection(L).First()) }
func selectionLast(L *lua.LState) int  { return pushSelection(L, checkSelection(L).Last()) }

func selectionFind(L *lua.LState) int {
	s := checkSelection(L)
	return pushSelection(L, s.Find(L.CheckString(2)))
}

func selectionHTML(L *lua.LState) int {
	s := checkSelection(L)
	ret, err := s.Html()
	if err != nil {
		L.RaiseError("%s", err.Error())
		return 0
	}
	L.Push(lua.LString(ret))
	return 1
}

func selectionText(L *lua.LState) int {
	L.Push(lua.LString(checkSelection(L).Text()))
	return 1
}

func checkSelection(L *lua.LState) *goquery.Selection {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*goquery.Selection); ok {
		return v
	}
	L.ArgError(1, "selection expected")
	return nil
}

func documentFind(L *lua.LState) int {
	p := checkDocument(L)
	return pushSelection(L, p.Find(L.CheckString(2)))
}

func checkDocument(L *lua.LState) *goquery.Document {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*goquery.Document); ok {
		return v
	}
	L.ArgError(1, "document expected")
	return nil
}
