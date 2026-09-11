## What this changes

<!-- One or two sentences. What is different for a builder using the site. -->

## Why

<!-- The problem it solves. Link an issue if there is one. -->

## Checked before opening

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` is clean
- [ ] `go test ./...` passes
- [ ] The flow this touches was exercised against a locally running server
- [ ] New pages are in both `pageNames` and `templates_test.go`
- [ ] No trophy STL files, no `data/` contents, no secrets
