# Contributing to sprue

sprue is a scale modelling platform: builders post what they have built, vote
for work they like, run their own competitions, and keep a private stash of
unbuilt kits. Contributions are welcome. Read this first, because the project
has firm opinions about how it is built.

## How a change gets in

Nobody pushes to `main`, maintainers included. Every change arrives as a pull
request and is reviewed before it merges.

1. Fork the repository and make a branch off `main`.
2. Make the change. Keep it to one thing.
3. Run the gates below and exercise the flow you touched against a running
   server.
4. Open a pull request and fill in the template.

The checks workflow runs the gates on your pull request. A first pull request
from a new contributor waits for a maintainer to release the workflow, which
is GitHub's protection for public repositories, so expect a short pause
before the checks start.

For anything large, open an issue first and describe the idea. A change that
does not fit the project's shape is a waste of your evening, and saying so
before you write it is the point of the issue.

## Running it locally

```sh
cd platform
go build -o sprue .
SPRUE_ADMIN_EMAIL=you@example.com ./sprue
```

Everything mutable lives in one data directory, `data` by default, set with
`SPRUE_DATA`. With no mail provider configured, sign-in links are printed to
the server log: enter your email on the site, then copy the link out of the
log. Signing in with the address in `SPRUE_ADMIN_EMAIL` makes that account
the admin. MAINTAINING.md has the rest of the operational detail.

## The gates

A change is not done until all four hold:

```sh
cd platform
gofmt -l .        # prints nothing
go vet ./...      # clean
go test ./...     # passes
```

and the flow you changed has been used against a locally running server, not
just covered by a test.

## Code standards

NASA's Power of Ten, in spirit, adapted to Go. No recursion. Every loop and
every collection bounded by a named cap, and hitting a cap returns a clean
error rather than growing. Functions short and readable top to bottom. Every
error checked. Inputs validated at the handler and again in the store. No
clever reflection or metaprogramming. No speculative abstraction. Natural
human names for variables.

One Go binary in `platform/`, standard library plus the pure-Go SQLite
driver, no framework. Server-rendered HTML templates and no JavaScript build
step. `main.go` is a thin shell; the logic lives in `store`, `images` and
`web`. Nothing outside `store` writes SQL.

## Templates and pages

`base.html` is the shell, `partials.html` holds the shared fragments, and
each page template defines `content`. Static URLs go through the `static`
template function so they carry the asset version. A new page goes in both
`pageNames` and `templates_test.go`, which renders every page with realistic
data.

## The look

Retro pixel game look: sky gradient behind a cream paper panel, dark ink
borders, RAF roundel blue and red as the only accents. NES.css supplies the
pixel borders, buttons and form controls, vendored in
`platform/web/static/vendor/`. `style.css` layers on the palette, the layout,
the fonts and the few components NES.css lacks. Fonts are self-hosted latin
subsets: Press Start 2P for headings, nav, buttons and tags, Pixelify Sans
for body text. The browser makes no external requests. Photos are never
pixelated, only the interface is. Nothing decorative without a function.

Where a browser draws its own control furniture, the site draws its own
instead, so a dropdown or a scrollbar does not arrive in Windows or macOS
style in the middle of a pixel page. Anything new in that line follows the
same rule, and still works with JavaScript turned off.

The pixel art is hand maintained: one path per colour on a pixel grid with
`shape-rendering="crispEdges"`. That covers the trophy badges, the airplane
marks, and the small page icons beside section titles. The five mark colours
must match `markColours` in `web/templates.go`, in the same order.

## Writing

Plain prose. No em dashes anywhere. No tables in the documentation. No
placeholder text. Say the thing and stop.

## What never goes in the repository

Trophy STL files, which live only in the data directory at `data/stl/`.
Anything from `data/`: the database, uploaded photos, avatars. Secrets and
`.env` files. Generated binaries.

## Scope

The hobby itself is never gamified. Votes curate, and a stash goal is the
member's own commitment. There are no points, no streaks and no badges for
activity. Placement badges and trophies are won in competitions, which is a
result, not a score. A proposal that adds a points system, a daily streak or
an engagement reward will be turned down, however well built it is.

## Reporting a security problem

Do not open a public issue. SECURITY.md says how.
