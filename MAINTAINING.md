# Maintaining sprue

sprue is a multi-user scale modelling platform: builders sign in with email magic links, post their builds with photos, vote for builds they like, and run their own competitions. Winners download a printable trophy from their own profile. One Go binary, one SQLite file, no admin console beyond the built-in admin page.

## The moving parts

The code lives in `platform/`. Everything mutable lives in one data directory set by `SPRUE_DATA` (default `data`):

```
data/
  sprue.db      SQLite database: users, sessions, builds, competitions, votes, trophies
  photos/           uploaded build photos, one folder per build
  avatars/          profile pictures
  stl/              first-place.stl, second-place.stl, third-place.stl (you put these here)
```

Trophy STL files are never in the repo and never at a public URL. Copy them into `data/stl/` yourself with exactly those names. The server hands them out only to a signed-in winner. How to design them is in TROPHIES.md.

## Running locally

```sh
cd platform
go build -o sprue .
SPRUE_ADMIN_EMAIL=you@example.com ./sprue
```

Sign-in requests are limited: one per email address and one per visitor address every 30 seconds, six per visitor address an hour, and 200 for the whole site a day so Brevo's free allowance of 300 cannot be spent by a bot. The form also carries a hidden field that people never see; a request that fills it is shown the usual "check your email" page and nothing is sent.

Without a mail provider configured, sign-in links are printed to the server log instead of emailed. Open the site, enter your email, copy the link from the log into the browser. Signing in with the address in `SPRUE_ADMIN_EMAIL` makes that account the admin.

Gates before calling any change done: `gofmt -l .` prints nothing, `go vet ./...` is clean, `go test ./...` passes, and the flow you touched works in a browser or with curl against a locally running server.

## Changing the look

The pixel look comes from three places. NES.css core is vendored at `platform/web/static/vendor/nes-core.min.css` and is never edited; upgrade it by replacing the file and its licence. `platform/web/static/style.css` holds the palette, layout, and every override, and is the only stylesheet you edit. The trophy badges and the airplane marks are plain SVG files under `platform/web/static/`, one path per colour on a pixel grid, and can be edited in any text editor or redrawn in a pixel editor. The mark comes in five colours. Clicking the plane in the header moves to the next colour and remembers it in a cookie; the colour list is `markColours` in `web/templates.go` and each entry needs a matching `mark-<colour>.svg`.

Static files are served with a one year cache and a version parameter derived from the stylesheet, so a new binary always gets fresh assets. Run `go test ./...` after any template change; the web tests render every page.

## Operations

`GET /healthz` returns `ok` when the database answers, for platform health checks. Every response carries a strict Content-Security-Policy (no scripts, same-origin styles, fonts, and images), nosniff, and frame denial. Text form posts are capped at 64KB; photo uploads at six files of 15MB each. Expired sessions and sign-in tokens are swept at startup and every hour, competitions advance by date in the same run, and due stash reminders go out. SIGTERM drains open requests for up to fifteen seconds before exit.

Builders can remove photos, choose the cover photo, and delete a build. A build that has entered a competition cannot be deleted because entries, votes, and trophies refer to it. Build IDs are never reused, so a link to a deleted build stays a 404.

## Environment variables

- `PORT`: listen port, default 8080. Railway and Render set this themselves.
- `SPRUE_DATA`: the data directory. In the container it defaults to `/data`; mount your persistent volume there.
- `SPRUE_URL`: the public base URL, used inside emailed sign-in links. Set it to your real domain in production.
- `SPRUE_ADMIN_EMAIL`: the email address that gets admin on sign-in.
- `SPRUE_BREVO_KEY`: a Brevo API key. When set, every email goes out over HTTPS through Brevo's transactional API from the address in `SPRUE_SMTP_FROM`, with replies directed to `SPRUE_SUPPORT_EMAIL` when that is set. This is the production path, because Railway blocks the SMTP ports on every plan below Pro.
- `SPRUE_SMTP_HOST`, `SPRUE_SMTP_PORT`, `SPRUE_SMTP_USER`, `SPRUE_SMTP_PASS`, `SPRUE_SMTP_FROM`: outbound email over SMTP, used when no Brevo key is set. Port 465 means TLS from the start, any other port means STARTTLS. `SPRUE_SMTP_FROM` is also the sender for Brevo. One of the Brevo key or the SMTP host is required unless `SPRUE_URL` is a localhost address. Locally, with neither set, sign-in links go to the log instead.
- `SPRUE_ANALYTICS_ID`: a Google Analytics measurement ID such as `G-XXXXXXXX`. Leave it unset and the only script served is the site's own small one. Set it and the pages also load Google's tag, with the Content-Security-Policy widened to allow exactly that and nothing else.
- `SPRUE_DISCORD_URL`: the invite link to the community Discord, shown in the footer. Unset, no link.
- `SPRUE_DISCORD_WEBHOOK`: a Discord channel webhook. When set, signed-in members get a feedback page whose messages post to that channel with their name and profile link. Unset, the page and its footer link do not exist.
- `SPRUE_SUPPORT_EMAIL`: an address for the footer's support link. Unset, no link.
- `SPRUE_KOFI_URL`: the site's Ko-fi page. When set, the footer gains a Donate link to a support page that lists the top supporters and everything given so far. Unset, no page and no link.
- `SPRUE_KOFI_TOKEN`: the verification token from Ko-fi's webhook settings. Ko-fi posts every payment to `/webhooks/kofi`; payments carrying this token are recorded, anything else is refused and logged. Unset, the webhook answers 404. Treat it as a secret.

## Donations

The site can run on donations through Ko-fi, which takes no fee of its own. Ko-fi calls the webhook once per payment with the donor's name, the amount, the currency, a message, and whether the donor ticked private. The store keeps those and nothing else, never the donor's email or payment details. Retries are harmless because each payment carries a transaction id and a repeat is ignored. Donations and memberships count; shop orders do not.

The support page lists the top twenty public supporters by total given, most first and earliest first on a tie, plus the sum of every gift including private ones. It updates itself as payments arrive. The admin page lists every recorded payment newest first with a Remove button, which is how to clear Ko-fi's test payment ("Jo Example") or a name you do not want on the site.

## Footer, terms and privacy

Every page ends with a footer: terms, privacy, licence, source, and, when configured, support, feedback, donate, and Discord, then a copyright line whose year is the current year. The terms and privacy pages are templates with a "last updated" date set by `policyUpdated` in `web/server.go`; change the date when you change the text. The text is plain and honest about what the site holds and why, but it is not legal advice.
- `SPRUE_VISION_KEY`: a Google Cloud Vision API key. When set, every uploaded photo is run through SafeSearch before it is saved and refused if likely adult or violent. Unset, photos are not screened.

## Profiles

Settings has a profile section: display name, a flair picked from seventeen small sprites (twelve planes, five bombs, from Kenney's Pixel Shmup pack, public domain, licence file alongside the images) plus the three trophy badges, which unlock when the member has placed first, second, or third in a competition, and an optional profile picture. New members get a plane by their ID until they choose. Pictures are cropped square, scaled to 256 pixels, re-encoded, screened when a Vision key is set, and stored under `avatars/` in the data directory with random names. The flair shows beside the name everywhere; the picture shows on the bench and in member lists.

Messages after an action appear as a toast at the top right, green with a tick or red with a cross, and leave after a few seconds or a click.

## Members and friends

The members page searches display names and slugs, case insensitively, and lists the newest members when the box is empty. Results are capped at fifty.

Friendship is a request one member sends from another's bench, which the other accepts or declines. The database holds one row per pair in either direction, so a second request from either side is refused. Three actions cover everything: request, accept, and remove, where remove also cancels a sent request, declines an incoming one, and ends a friendship. Each member's friends page shows requests waiting on them, requests they have sent, and their friends, and nobody else can see it. The account menu shows a count while requests are waiting. Friendship changes nothing about what a member can see; private builds stay private.

## Private builds

A member can keep a build private when adding or editing it. Private builds show only on the owner's own bench, marked with a tag, and their page returns not found to anyone else. They are left out of the community page, the featured spot, the sitemap, and link previews, and they cannot enter a competition. A build already in a competition cannot be made private, because its entry is public.

## Reports and hidden builds

Any signed-in member can report a build that is not their own, with an optional reason, once per build. The admin page lists reported builds with the count and the latest reason. Hide takes a build out of the community page, the featured spot, member pages, competition entry lists, and the sitemap, and its page returns not found to everyone except the owner and the admin, who see a notice. Unhide reverses it. Dismiss clears the reports. Nothing is deleted by the admin; the owner can still delete their own build if it is not in a competition.

## Schema changes

Tables are created on first start and never altered. Adding a table is safe. Adding a column to an existing table needs a migration step, because an existing database will not get it. Nothing has needed one before launch; add a versioned migration in the store before the first such change afterwards.

## The stash

Each member has a private stash page listing kits they own and have not built, with brand, scale, and cost. The page opens with the member's model debt: how many kits are waiting, what they cost in total, and how long the oldest has sat. One kit can be marked as next. A goal is a number of kits and a date, and every kit finished from the stash after the goal was set counts toward it. Each kit has a journal of short dated notes. Finishing a kit creates a build with the kit's details and sends the member to add photos; the stash entry keeps its journal and links to the build.

Members can opt into a weekly or monthly reminder email in settings. The hourly housekeeping run sends up to fifty due reminders, each a plain text note with the debt line, the next kit, and the goal. A member is marked as reminded before the send is attempted, so a bouncing address is not retried every hour. Nothing is sent when the stash is empty and no goal is set. There are no points, streaks, or badges anywhere; the stash exists to make unbuilt kits visible and to hold a commitment the member chose.

## Search engines and link previews

`/robots.txt` allows everything public and keeps crawlers out of sign-in, account, form, and admin pages. `/sitemap.xml` is built from the database on request and lists the front page, every competition, every member page, and every build, newest first, capped at five thousand of each. Submit it in Search Console once and it stays current.

Every page carries a description, a canonical URL, and Open Graph tags, so links pasted into chat or social apps show a title, a line of text, and an image. Build pages use their cover photo; everything else uses the plane on paper, which is also the home screen icon on phones. Sign-in, settings, forms, admin, and error pages are marked noindex.

## The featured spot

Any signed-in member can vote for a build from its page, one vote per build, never for their own, and can take the vote back. The community page shows up to three builds with the most votes cast in the last 30 days. Nothing else is derived from votes: no scores, no rankings of people.

## One full competition

Everything happens in the browser and by date. Make sure the three STL files are in `data/stl/` on the server volume before the first competition ends.

Any member starts one from the competitions page: a title, a category from a fixed list (fighter, heavy bomber, medium bomber, tank, other), a brief, the last day for entries (within 180 days) and the last day for voting (within 60 days after that). A member can have three competitions running at once. It starts `open`. Builders enter by picking one of their own builds on the competition page. One entry per builder, sixty four entries at most.

The day after entries close it moves to `voting` on its own. Signed-in builders get one vote each and cannot vote for their own entry. Vote counts stay hidden until the competition is decided.

The day after voting closes it is decided on its own. The tally is deterministic, ties break in favour of the earlier entry, and entries with zero votes never place. With no votes at all the competition closes with no placings. Placings become trophies: first, second, and third get the gold, silver, and bronze badges on the competition page, on their build cards, and in the trophy case on their profile.

A decided competition shows its podium: the three placing builds with cover, badge, title, and builder, first in the middle. The past winners page lists every decided competition with its podium, newest first. The first time a winner loads any page after a decision, a toast congratulates them once and points at their profile.

The admin page can decide a competition early while it is in voting, and that is the only status control anyone has. Status moves happen in the hourly housekeeping run and whenever a competition page is opened.

## Sprue competitions

Alongside community competitions there are the site's own four, all Second World War, each starting itself once a month on the first day of its kind: Moderate Mitchell for medium and light bombers on the first Monday, Tanktastic for tanks and self-propelled guns on the first Tuesday, Fighting Friday for fighters on the first Friday, and Big Bomber for heavy bombers on the first Saturday. Each runs three weeks of entries and one week of voting, is titled with its month, and belongs to the admin account. The hourly housekeeping run starts them; it never starts the same brief twice in a month, and it does nothing until an admin account exists. They carry a purple "sprue" tag. Community competitions, started by members from the competitions page, have no sprue tag and no fixed brief. Every competition carries a category tag, and the competitions page filters by category so a member can find one to enter. The category list lives in the store.

The brief is checked in three places. It is shown on the competition page above the entries. Every entrant, in any competition, must tick that their build meets the brief before the entry is accepted. And while a competition is open or voting, the admin sees a remove link on each entry and can take out one that misses the brief, along with any votes it had. Decided competitions keep their entries.

The briefs, their weekdays, and the three plus one week lengths live in `web/competitions.go`; changing the schedule is changing a line there.

Winners download their own trophy: each winner sees a `download your trophy STL` link on their profile. It works once. If a download goes wrong, the Admin page lists every trophy with a `re-arm` button that grants one more download. Remind winners the trophy prints in plain grey on purpose: gold DB0016 for first, silver DB0011 for second, antique bronze DB0171 for third.

## Deployment on Railway or Render

The Dockerfile at the repo root builds the platform. Set up the service with:

- a persistent volume mounted at `/data`
- `SPRUE_URL` set to your domain
- `SPRUE_ADMIN_EMAIL` set to your email
- the mail variables pointed at your email provider

Copy the three STL files into the volume once. Everything else, including the database, lives on that volume, so redeploys lose nothing.

## Honest limits

Magic links and votes are rate limited per address and email, and every upload is decoded and re-encoded server side, capped at 8 MB in and 1600 pixels on the long edge. Fixed caps everywhere: 200 builds per user, 12 photos per build, 64 entries per competition, 256 competitions. Hitting a cap is a clean error, never growth. Vote integrity is one account one vote; someone determined enough to register many email addresses can still cheat, and if that ever matters the next step is approving new accounts before they can vote.

