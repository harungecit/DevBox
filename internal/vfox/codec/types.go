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

// Package codec converts Go values to Lua tables and back the way vfox does,
// so vfox plugins see exactly the field names they were written against.
// Ported from github.com/version-fox/vfox internal/plugin/luai/codec
// (Apache-2.0) — see THIRD_PARTY_NOTICES.md.
package codec

const (
	NavigatorObjKey = "VFOX_NAVIGATOR"
)

type Navigator struct {
	UserAgent string `json:"userAgent"`
}
