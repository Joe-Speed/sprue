# Maintaining sprue

sprue is a multi-user scale modelling platform: builders sign in with email magic links, post their builds with photos, vote for builds they like, and run their own competitions. Winners download a printable trophy from their own profile. One Go binary, one SQLite file, no admin console beyond the built-in admin page.

## The moving parts

The code lives in `platform/`. Everything mutable lives in one data directory set by `SPRUE_DATA` (default `data`):

```
data/
  sprue.db      SQLite database: users, sessions, builds, competitions, votes, trophies
  photos/           uploaded build photos, one folder per build
  stl/              first-place.stl, second-place.stl, third-place.stl (you put these here)
```

Trophy STL files are never in the repo and never at a public URL. Copy them into `data/stl/` yourself with exactly those names. The server hands them out only to a signed-in winner. How to design them is in TROPHIES.md.

## Running locally

```sh
cd platform
go build -o sprue .
SPRUE_ADMIN_EMAIL=you@example.com ./sprue
```

Without SMTP configured, sign-in links are printed to the server log instead of emailed. Open the site, enter your email, copy the link from the log into the browser. Signing in with the address in `SPRUE_ADMIN_EMAIL` makes that account the admin.

Gates before calling any change done: `gofmt -l .` prints nothing, `go vet ./...` is clean, `go test ./...` passes, and the flow you touched works in a browser or with curl against a locally running server.

## Changing the look

The pixel look comes from three places. NES.css core is vendored at `platform/web/static/vendor/nes-core.min.css` and is never edited; upgrade it by replacing the file and its licence. `platform/web/static/style.css` holds the palette, layout, and every override, and is the only stylesheet you edit. The trophy badges and the airplane marks are plain SVG files under `platform/web/static/`, one path per colour on a pixel grid, and can be edited in any text editor or redrawn in a pixel editor. The mark comes in five colours. Clicking the plane in the header moves to the next colour and remembers it in a cookie; the colour list is `markColours` in `web/templates.go` and each entry needs a matching `mark-<colour>.svg`.

Static files are served with a one year cache and a version parameter derived from the stylesheet, so a new binary always gets fresh assets. Run `go test ./...` after any template change; the web tests render every page.

## Operations

`GET /healthz` returns `ok` when the database answers, for platform health checks. Every response carries a strict Content-Security-Policy (no scripts, same-origin styles, fonts, and images), nosniff, and frame denial. Text form posts are capped at 64KB; photo uploads at six files of 8MB each. Expired sessions and sign-in tokens are swept at startup and every hour. SIGTERM drains open requests for up to fifteen seconds before exit.

Builders can remove photos, choose the cover photo, and delete a build. A build that has entered a competition cannot be deleted because entries, votes, and trophies refer to it. Build IDs are never reused, so a link to a deleted build stays a 404.

## Environment variables

- `PORT`: listen port, default 8080. Railway and Render set this themselves.
- `SPRUE_DATA`: the data directory. In the container it defaults to `/data`; mount your persistent volume there.
- `SPRUE_URL`: the public base URL, used inside emailed sign-in links. Set it to your real domain in production.
- `SPRUE_ADMIN_EMAIL`: the email address that gets admin on sign-in.
- `SPRUE_SMTP_HOST`, `SPRUE_SMTP_PORT`, `SPRUE_SMTP_USER`, `SPRUE_SMTP_PASS`, `SPRUE_SMTP_FROM`: outbound email. The host is required unless `SPRUE_URL` is a localhost address. Locally, with no host set, sign-in links go to the log instead.

## The featured spot

Any signed-in member can vote for a build from its page, one vote per build, never for their own, and can take the vote back. The workbench shows up to three builds with the most votes cast in the last 30 days. Nothing else is derived from votes: no scores, no rankings of people.

## One full competition

Everything happens in the browser and by date. Make sure the three STL files are in `data/stl/` on the server volume before the first competition ends.

Any member starts one from the competitions page: a title, a brief, the last day for entries (within 180 days) and the last day for voting (within 60 days after that). A member can have three competitions running at once. It starts `open`. Builders enter by picking one of their own builds on the competition page. One entry per builder, sixty four entries at most.

The day after entries close it moves to `voting` on its own. Signed-in builders get one vote each and cannot vote for their own entry. Vote counts stay hidden until the competition is decided.

The day after voting closes it is decided on its own. The tally is deterministic, ties break in favour of the earlier entry, and entries with zero votes never place. With no votes at all the competition closes with no placings. Placings become trophies: first, second, and third get the gold, silver, and bronze badges on the competition page, on their build cards, and in the trophy case on their profile.

The admin page can decide a competition early while it is in voting, and that is the only status control anyone has. Status moves happen in the hourly housekeeping run and whenever a competition page is opened.

Winners download their own trophy: each winner sees a `download your trophy STL` link on their profile. It works once. If a download goes wrong, the Admin page lists every trophy with a `re-arm` button that grants one more download. Remind winners the trophy prints in plain grey on purpose: gold DB0016 for first, silver DB0011 for second, antique bronze DB0171 for third.

## Deployment on Railway or Render

The Dockerfile at the repo root builds the platform. Set up the service with:

- a persistent volume mounted at `/data`
- `SPRUE_URL` set to your domain
- `SPRUE_ADMIN_EMAIL` set to your email
- the SMTP variables pointed at your email provider

Copy the three STL files into the volume once. Everything else, including the database, lives on that volume, so redeploys lose nothing.

## Honest limits

Magic links and votes are rate limited per address and email, and every upload is decoded and re-encoded server side, capped at 8 MB in and 1600 pixels on the long edge. Fixed caps everywhere: 200 builds per user, 12 photos per build, 64 entries per competition, 256 competitions. Hitting a cap is a clean error, never growth. Vote integrity is one account one vote; someone determined enough to register many email addresses can still cheat, and if that ever matters the next step is approving new accounts before they can vote.

