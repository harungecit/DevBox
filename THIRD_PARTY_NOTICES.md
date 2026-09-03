# Third-party notices

DevBox is licensed under the MIT License (see `LICENSE`). It bundles or
adapts the following third-party work.

## vfox (Apache License 2.0)

Copyright 2026 Han Li and contributors — https://github.com/version-fox/vfox

DevBox's vfox plugin support (`internal/vfox`) reproduces vfox's plugin
contract so that plugins from the vfox registry run unchanged. The following
files were ported from vfox and keep their original Apache-2.0 license header:

- `internal/vfox/codec/encode.go`, `internal/vfox/codec/decode.go`,
  `internal/vfox/codec/types.go` — Go ⇄ Lua value conversion
  (from `internal/plugin/luai/codec`)
- `internal/vfox/modules/json.go` — `require("json")`
  (from `internal/plugin/luai/module/json`, itself a fork of
  github.com/layeh/gopher-json, MIT)
- `internal/vfox/modules/html.go` — `require("html")`
  (from `internal/plugin/luai/module/html`)
- `internal/vfox/modules/strings.go` — `require("vfox.strings")`
  (from `internal/plugin/luai/module/string`)
- `internal/vfox/archive/archive.go` — the archive root-stripping rules
  (from `internal/shared/util/decompressor.go`)

The hook contract (`internal/vfox/hooks.go`) mirrors vfox's
`internal/plugin/model.go` field names. Everything else in `internal/vfox`
was written for DevBox.

A copy of the Apache License 2.0 is available at
https://www.apache.org/licenses/LICENSE-2.0.

## vfox plugins

Plugins installed through the runtime catalog are downloaded from
https://github.com/version-fox/vfox-plugins (or the source the user provides)
at the user's request. Each plugin carries its own license, shown in DevBox
before installation; they are not distributed with DevBox.

## Go modules

- github.com/yuin/gopher-lua — MIT
- github.com/PuerkitoBio/goquery — BSD-3-Clause
- github.com/andybalholm/cascadia — BSD-2-Clause
- github.com/ulikunitz/xz — BSD-3-Clause
- github.com/wailsapp/wails/v2 — MIT
- github.com/energye/systray — Apache-2.0
- golang.org/x/sys — BSD-3-Clause

## Icons

Logos for plugin runtimes under `frontend/src/assets/images/icons/plugin-*.svg`
come from Simple Icons (https://simpleicons.org, CC0 1.0). Brand names and
logos remain the property of their respective owners.
