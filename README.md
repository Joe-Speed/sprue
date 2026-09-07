# sprue

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)
[![SQLite](https://img.shields.io/badge/SQLite-pure_Go-003B57?logo=sqlite&logoColor=white)](https://pkg.go.dev/modernc.org/sqlite)
[![No JS framework](https://img.shields.io/badge/frontend-server_rendered-brightgreen)]()

A community platform for scale modellers. Builders sign in with an email link, post their builds with photos, curate their own bench page, vote for the builds they like, and run their own competitions. Winners download a 3D-printable trophy that arrives, like everything else in this hobby, unpainted.

Built as a single Go binary with SQLite. No framework, no JavaScript build step, no external services beyond outbound email. One process, one data directory, done.

## Features

- **Passwordless sign-in.** Email magic links, single use, fifteen minute expiry. No passwords stored, ever.
- **Your bench, your page.** Every builder gets a profile at `/u/name` with a trophy case, pinned builds up top, and the rest ordered by build date.
- **Community featured spot.** Members vote for builds they like, one vote per build each and never for their own. The most voted builds of the last 30 days sit at the top of the workbench.
- **Builds with photos.** Upload up to six at a time, pick the cover, remove the ones you do not want. Uploads are decoded and re-encoded server side, stripped of metadata, and capped at 1600 pixels on the long edge. Nothing a browser sends is stored verbatim.
- **Competitions, run by members.** Anyone can start one with a brief, an entry close date and a voting close date. Entries open, then voting, then results, all by date. Enter one of your own builds, one entry per builder. One vote per account, no voting for yourself, and vote counts stay hidden until the result is decided.
- **Deterministic results.** Ties break to the earlier entry, entries with zero votes never place, and the tally is reproducible from the database.
- **Printable trophies.** First, second, and third get placement badges on their builds and profile, plus a single-use download of their trophy STL. The STL files live only on the server and are released only to their winner. Recommended paints: gold DB0016, silver DB0011, antique bronze DB0171.
- **Retro pixel look.** NES.css borders and controls, self-hosted pixel fonts, a sky and paper palette, and pixel-art trophy badges. Photos stay photographs. No external requests from the browser.
- **Bounded by design.** Every collection has a hard cap and every limit is a clean error, never growth. The code follows the spirit of NASA's Power of 10 rules, adapted to Go.

## Quick start

Requires Go 1.26 or newer.

```sh
git clone https://github.com/Joe-Speed/sprue.git
cd sprue/platform
go build -o sprue .
SPRUE_ADMIN_EMAIL=you@example.com ./sprue
```

Open http://localhost:8080, enter your email, and copy the sign-in link from the server log (with no SMTP configured, links are logged instead of emailed, which is the development mode). Signing in with the admin email gives that account the admin pages.

Run the tests:

```sh
go test ./...
```

## Configuration

All configuration is environment variables. None of them are secrets you would ever commit.

- `PORT` sets the listen port, default 8080.
- `SPRUE_DATA` sets the data directory, default `data`. The SQLite database, uploaded photos, and trophy STL files all live here and nowhere else.
- `SPRUE_URL` is the public base URL used inside emailed sign-in links.
- `SPRUE_ADMIN_EMAIL` grants admin to that address on sign-in.
- `SPRUE_SMTP_HOST`, `SPRUE_SMTP_PORT`, `SPRUE_SMTP_USER`, `SPRUE_SMTP_PASS`, `SPRUE_SMTP_FROM` configure outbound email. Unset means development mode.

## Deployment

The Dockerfile at the repo root builds a small static image. Point a persistent volume at `/data`, set the environment variables above, and copy your three trophy STL files into the volume at `stl/`. Railway and Render both work out of the box; the full operator walkthrough, including running a competition end to end, is in [MAINTAINING.md](MAINTAINING.md).

## Privacy and data

Everything user-generated stays in the data directory: the database, photos, and trophies. The repository contains code and badge artwork only. There are no analytics, no third-party scripts, no tracking, and the only personal data stored is an email address, a display name, and whatever a builder writes about their models. Session and sign-in tokens are stored hashed.

## License

sprue is released under the [GNU AGPL v3](LICENSE). You are free to run, study, and modify it; if you run a modified version as a public service, you must offer your changes under the same terms. Copyright remains with the author, who may offer the software under other terms.
