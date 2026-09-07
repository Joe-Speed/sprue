# sprue

A multi-user scale modelling platform reflecting Joe's passion for the hobby. Builders sign in with email magic links, post builds with photos from the community workbench, and organise their bench (pinned first, then by date). Members vote for builds they like and the most voted in the last 30 days are featured on the workbench. Any member can start a competition with an entry date and a voting date; status advances by date with no admin step. Winners get placement badges and download their 3D-printable trophy STL, which is never in the repo and never public. No gamification: votes curate, they never score people.

## Architecture

One Go binary in `platform/`, standard library plus the pure-Go SQLite driver, no framework. Server-rendered HTML templates, no JavaScript build step. All mutable state (SQLite database, uploaded photos, trophy STLs) lives in one data directory from `SPRUE_DATA`, mounted as a volume in deployment. `main.go` is a thin shell; logic lives in `store`, `images`, and `web` packages. Nothing outside `store` writes SQL.

## Code standards

NASA Power of 10 spirit, adapted to Go: no recursion, every loop and collection bounded by a named cap, functions short and readable top to bottom, every error checked, inputs validated at the handler and again in the store, no clever reflection or metaprogramming, no speculative abstraction. Hitting a cap returns a clean error, never growth. Natural human names for variables.

## Gates

Before declaring any change done: `gofmt -l .` prints nothing, `go vet ./...` clean, `go test ./...` passes, and the affected flow is exercised against a locally running server.

## Style rules

No em dashes anywhere. No tables in docs. Plain prose. Never commit or push; Joe runs git himself.

## Look

Retro pixel game look. Sky gradient behind a cream paper panel, dark ink borders, RAF roundel blue and red as the only accents. NES.css core (vendored, MIT, in `platform/web/static/vendor/`) supplies the pixel borders, buttons, and form controls; `style.css` layers the palette, layout, fonts, and the few components NES.css lacks. Fonts are self-hosted latin subsets: Press Start 2P for headings, nav, buttons, tags; Pixelify Sans for body text. No external requests from the browser. Photos are never pixelated, only the UI is. No decorative elements without a function.

## Pixel art

The trophy badges (`platform/web/static/trophies/`) and the airplane marks (`platform/web/static/mark-<colour>.svg`) are hand-maintained SVGs: one path per colour on a pixel grid with `shape-rendering="crispEdges"`. The five mark colours must match `markColours` in `web/templates.go`, in the same order. Paints for the printed trophies: gold DB0016 first, silver DB0011 second, antique bronze DB0171 third. Trophy STL files belong only in the data directory at `data/stl/`, never in the repo.

## Templates

`base.html` is the shell, `partials.html` holds shared fragments (build cards, place badges, status tags), and each page template defines `content`. Static URLs go through the `static` template function so they carry the asset version and can be cached for a year. `templates_test.go` renders every page with realistic data; add new pages to both `pageNames` and that test.
