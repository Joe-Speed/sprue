# Maintaining sprue

sprue is a multi-user scale modelling platform: builders sign in with email magic links, put up their builds with photos, and enter competitions. The community votes, and winners download a printable trophy from their own profile. One Go binary, one SQLite file, no admin console beyond the built-in admin pages.

## The moving parts

The code lives in `platform/`. Everything mutable lives in one data directory set by `SPRUE_DATA` (default `data`):

```
data/
  sprue.db      SQLite database: users, sessions, builds, competitions, votes, trophies
  photos/           uploaded build photos, one folder per build
  stl/              first-place.stl, second-place.stl, third-place.stl (you put these here)
```

Trophy STL files are never in the repo and never at a public URL. Copy them into `data/stl/` yourself with exactly those names. The server hands them out only to a signed-in winner.

## Running locally

```sh
cd platform
go build -o sprue .
SPRUE_ADMIN_EMAIL=you@example.com ./sprue
```

Without SMTP configured, sign-in links are printed to the server log instead of emailed. Open the site, enter your email, copy the link from the log into the browser. Signing in with the address in `SPRUE_ADMIN_EMAIL` makes that account the admin.

Gates before calling any change done: `gofmt -l .` prints nothing, `go vet ./...` is clean, `go test ./...` passes, and the flow you touched works in a browser or with curl against a locally running server.

## Environment variables

- `PORT`: listen port, default 8080. Railway and Render set this themselves.
- `SPRUE_DATA`: the data directory. In the container it defaults to `/data`; mount your persistent volume there.
- `SPRUE_URL`: the public base URL, used inside emailed sign-in links. Set it to your real domain in production.
- `SPRUE_ADMIN_EMAIL`: the email address that gets admin on sign-in.
- `SPRUE_SMTP_HOST`, `SPRUE_SMTP_PORT`, `SPRUE_SMTP_USER`, `SPRUE_SMTP_PASS`, `SPRUE_SMTP_FROM`: outbound email. Leave the host unset and links go to the log, which is only useful in development.

## One full competition

Everything happens in the browser.

First, make sure the three STL files are in `data/stl/` on the server volume.

Create it: sign in as admin, open Admin, give the competition a title and a deadline, create. It starts in `open`. Builders enter by picking one of their own builds on the competition page. One entry per builder, sixty four entries at most.

Open voting: on the Admin page press `open voting`. Signed-in builders get one vote each, and cannot vote for their own entry. Vote counts stay hidden until the competition is decided. If entries need more time, `back to open` reverses it.

Close and decide: you pick the moment; the deadline is information, nothing enforces it. Press `close and decide`. The tally is deterministic, ties break in favour of the earlier entry, and entries with zero votes never place. Placings become trophies: first, second, and third get the gold, silver, and bronze badges on the competition page, on their build cards, and in the trophy case on their profile.

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

