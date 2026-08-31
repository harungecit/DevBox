## What does this PR do?

<!-- A short summary; link the issue if there is one. -->

## Checklist

- [ ] `go build ./...`, `go vet ./...` and `go test ./...` pass
- [ ] Cross-compiles: `GOOS=windows go build ./...` and `GOOS=darwin go build ./...`
- [ ] `cd frontend && npm run check` passes
- [ ] New user-visible strings added to **both** `en.json` and `tr.json`
- [ ] OS-specific behavior goes through `internal/platform/` (no raw syscalls in feature code)
- [ ] `frontend/wailsjs/` was regenerated (not hand-edited) if App methods changed
