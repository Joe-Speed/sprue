# benchtime

A multi-user scale modelling platform reflecting Joe's passion for the hobby. Builders sign in with email magic links, share builds with photos, organise their profile (featured first, then by date), and enter mini competitions with an Airfix focus. Winners get placement badges and download their 3D-printable trophy STL, which is never in the repo and never public.

## Architecture

One Go binary in `platform/`, standard library plus the pure-Go SQLite driver, no framework. Server-rendered HTML templates, no JavaScript build step. All mutable state (SQLite database, uploaded photos, trophy STLs) lives in one data directory from `BENCHTIME_DATA`, mounted as a volume in deployment. `main.go` is a thin shell; logic lives in `store`, `images`, and `web` packages. Nothing outside `store` writes SQL.

## Code standards

NASA Power of 10 spirit, adapted to Go: no recursion, every loop and collection bounded by a named cap, functions short and readable top to bottom, every error checked, inputs validated at the handler and again in the store, no clever reflection or metaprogramming, no speculative abstraction. Hitting a cap returns a clean error, never growth. Natural human names for variables.

## Gates

Before declaring any change done: `gofmt -l .` prints nothing, `go vet ./...` clean, `go test ./...` passes, and the affected flow is exercised against a locally running server.

## Style rules

No em dashes anywhere. No tables in docs. Plain prose. Never commit or push; Joe runs git himself.

## Trophies

Placement badges live in `assets/trophies/` (source) and `platform/web/static/trophies/` (embedded copies; keep in sync). Paints: gold DB0016 first, silver DB0011 second, antique bronze DB0171 third. Trophy STL files belong only in the data directory at `data/stl/`.
