# Audit, 10 September 2026

A full read of every Go file, template, stylesheet hook, the Dockerfile, and the deployment docs at commit a7332a5, plus the uncommitted working tree changes present at the time (a sign-in honeypot, an hourly per visitor cap of six sign-in emails, and a daily site wide ceiling of 200, in `auth.go`, `server.go`, `login.html`, and `auth_test.go`). Each finding below was confirmed by reading the code path end to end and, where it could be, by running it. Things that looked wrong but turned out fine are listed at the end so nobody re-checks them.

## Size

The platform is 5348 lines of Go outside tests, 1798 lines of tests, 750 lines of templates, 1395 lines of CSS, and 67 lines of JavaScript. That covers 58 routes: magic link sign-in, sessions, uploads with server side re-encoding, community voting, member and sprue competitions with automatic advancement, trophies, stash and goals with reminder mail, friends, moderation, Ko-fi donations, two mail providers, SafeSearch screening, SEO, and an admin page. For that scope the code is lean, not bloated. Tests are a quarter of the Go and render every page.

Two places are worth tightening but neither is a problem. `store/store.go` at 1345 lines is the only long file; it splits cleanly into users, builds, competitions, and trophies without changing a line of logic. `EntriesWithVotes` repeats the scan list from `scanBuilds` by hand, so the two column lists must be edited together; a shared scan helper removes that trap. `Process` and `ProcessSquare` in `images` share most of their body. No dead code was found.

## High

### 1. An unrelated PDF is committed to the public repository

`WEX Pricing Quant Survey.pdf` (188 KB) sits at the repo root, was added in commit a7332a5, and is on `origin/main` at github.com/Joe-Speed/sprue, which the site footer links to as the source. Nothing in the project references it. If it is confidential, deleting it in a new commit is not enough; it stays in history and needs a rewrite of that commit and a force push, plus treating it as disclosed. Add `*.pdf` to `.gitignore` afterwards so this cannot repeat.

### 2. Image decoding allocates before the size check

`images.Process` and `images.ProcessSquare` call `image.Decode` first and only then compare the bounds against `MaxSourcePixels`. Go's PNG and JPEG decoders allocate the full pixel buffer from the header before reading any pixel data, so a tiny file that declares enormous dimensions forces the allocation before the check runs. Measured with a test against the real functions:

```
png 20000x20000:  file bytes 72,  allocated during decode 1526 MB
jpeg 60000x60000: file bytes 130, allocated during decode 5149 MB
```

Both return "not a decodable image" afterwards, but the memory has already been taken. Any signed-in member can send six such files per request and there is no cap on concurrent uploads, so a handful of parallel requests exhausts the Railway container and the process is killed. The 15 MB body limit does not help because the files are under 200 bytes.

Fix: call `image.DecodeConfig` on the bytes first, reject anything whose width, height, or product exceeds the caps, and only then call `image.Decode`. Add a small semaphore (two or three slots) around decoding so concurrent uploads queue instead of stacking allocations. Both changes belong in `images` so every caller gets them.

## Medium

### 3. A build made from a stash kit cannot be deleted

`FinishStashItem` sets `stash.build_id` to the new build. `DeleteBuild` removes photos, votes, and reports and then deletes the build row, but never touches the stash row. With `foreign_keys(1)` on, the delete fails. Confirmed with a test: create a stash kit, finish it, delete the build, and the store returns `constraint failed: FOREIGN KEY constraint failed (787)`. The handler maps that to a 404 "Not your build.", so the member sees a wrong message and has no way to delete the build.

Fix: inside the `DeleteBuild` transaction, run `update stash set build_id = null where build_id = ?` before deleting the build. Decide whether the kit should also return to unbuilt; leaving it as built with no link is the simpler reading. Add the test above to `store_test.go`.

### 4. The client address is taken from the first X-Forwarded-For entry

`clientKey` returns the first comma separated value of `X-Forwarded-For`. Behind Railway and Cloudflare, a client that sends its own header gets it prepended and the proxy appends the real address after it, so the first entry is whatever the client chose. Every request can carry a fresh value, which defeats every per address limit: the 30 second gap and the new hourly cap of six on sign-in emails, and the 5 second gap on competition votes. The per email gap still holds, so the attacker uses a different address each time.

The uncommitted daily ceiling of 200 sign-in emails stops the site being used as a mail cannon, but it turns the same spoofing into a one line lockout: 200 requests with made up addresses and forwarded headers, and nobody can sign in for the rest of the day. The honeypot does not help because a scripted client simply leaves the field empty.

Fix: behind Cloudflare use `CF-Connecting-IP`; otherwise take the last entry of `X-Forwarded-For` (the one the trusted proxy appended) rather than the first. With a trustworthy address the hourly cap of six does its job and the daily ceiling becomes a backstop rather than the first thing an attacker reaches. Consider exempting addresses that already have an account from the daily count, so a bot cannot lock out existing members.

### 5. The shared rate limiter locks everyone out when full

`rateLimiter.allow` keeps at most 4096 keys. When the map is full it evicts only entries older than an hour, and if none qualify it returns false for every caller. Sign-in adds two keys per attempt (one for the email, one for the address). So 2048 sign-in attempts with distinct addresses and emails inside an hour fill the table, and from then until entries age out every sign-in, vote, and feedback post on the site is refused. With finding 4 the distinct addresses cost nothing. The new `quota` type in the working tree has the same shape and the same refuse-when-full behaviour.

Fix: evict the oldest entry when full instead of refusing, and give sign-in, votes, and feedback separate limiters so one cannot starve the others.

### 6. Photo uploads can exceed the server read timeout

`http.Server` has `ReadTimeout` 60 seconds, which covers the whole request body. Photo posts accept six files of 15 MB, up to 90 MB. On a 10 Mbps mobile uplink that takes about 75 seconds, so the connection is cut before the handler runs, nothing is saved, and the member sees a browser error rather than a site message. Phone photos are commonly 3 to 8 MB, so this hits real uploads of a few large files, not only the worst case.

Fix: in the three upload handlers call `http.NewResponseController(w).SetReadDeadline` with a longer deadline before parsing the form, and leave the server default for everything else.

### 7. Go standard library vulnerabilities reachable from this code

`govulncheck` against go1.26.5 reports six standard library issues on paths this code uses (`net/http` server and client, `html/template`, `crypto/tls`, `net/url`, `encoding/asn1`), all fixed in go1.26.6. The Dockerfile builds from `golang:1.26-alpine`, which floats to the newest patch, so a fresh deploy picks the fix up; the local toolchain does not and needs updating. `modernc.org/sqlite` is at v1.34.4 with v1.58.0 current; govulncheck found nothing reachable in it, but two years of fixes is a long gap for the storage engine. Update both and re-run the gates.

## Low

### 8. Default names and permanent slugs are the email local part

`FindOrCreateUser` sets the display name and the slug to everything before the `@`. The slug is permanent (settings says so) and appears in the members list, every build card, the sitemap, and friend requests. For the common `firstname.lastname@` address this publishes most of the email address to everyone including search engines, and renaming the display name does not change the URL. Fix: generate a neutral default (for example `builder-` plus a short random suffix) and let the member choose a name, and a slug, on first sign-in.

### 9. Magic links are consumed by a GET

`handleAuthVerify` burns the token on the GET request. Corporate mail gateways, Outlook Safe Links, and some antivirus products fetch every link in an email before the user sees it, which uses the token and leaves the member with "expired or already used". Fix: have the GET render a page with a single "Sign in" button that posts the token, and consume it on the POST. While there, make `ConsumeMagicToken` a single conditional update (`where token_hash = ? and used = 0 and expires_at > ?`) and check the row count instead of select then update.

### 10. Anyone can put text into the success toast

`renderMeta` reads `note` and `error` from the query string and shows them in the green or red toast on any page. The text is escaped, so there is no script injection, but a link such as `/settings?note=Your+account+was+suspended,+pay+at+...` shows that sentence as a site message with a tick beside it. Fix: carry flash messages in a short lived cookie set by the redirecting handler, or pass a code and look the text up server side.

### 11. Photos and avatars stay served after a build is hidden or made private

`handlePhoto` and `handleAvatar` serve any file whose name matches the pattern without checking the build. Names are 96 bits of randomness, so guessing is not the concern; already shared URLs are. When the admin hides a build for its content, the image keeps loading everywhere it was pasted, and the cover keeps working in link previews cached by Discord and the like. Fix: in `handlePhoto`, load the build and apply `visibleTo` before serving; it is one indexed query.

### 12. Admin rights are granted but never withdrawn

Sign-in sets `is_admin` when the email matches `SPRUE_ADMIN_EMAIL` and never clears it. Changing the variable, or a typo in it that is later corrected, leaves the old account an admin for good with no way to remove it except editing the database. Fix: on each sign-in set the flag to match the configuration in both directions, or document the manual step.

### 13. Caps are checked outside the insert transaction

`CreateBuild`, `AddPhoto`, `CreateStashItem`, `AddJournalEntry`, `EnterCompetition`, `RequestFriend`, and `AddDonation` all count rows, compare against the cap, then insert as a separate statement. Two concurrent requests can both pass the count and land one over the cap. The single database connection keeps the window very small and the overshoot is one or two rows, so this is a correctness note against the "bounded" rule rather than a live problem. Fix when convenient: do the count and insert inside one transaction.

### 14. MAINTAINING.md has drifted from the code

The "Honest limits" section says uploads are capped at 8 MB; the code allows 15 MB. "Operations" says the Content-Security-Policy allows no scripts; `site.js` is served and allowed. "Changing the look" says the asset version derives from the stylesheet; it is a hash of every static file. Small, but the file is the operator's reference.

## Checked and sound

These were examined because they are the usual places things go wrong, and they hold up.

Sessions and magic tokens are 256 bit random values stored only as SHA-256 hashes, with expiry enforced on read and swept hourly. The CSRF token is derived from the session secret and checked on every POST that changes state; cookies are HttpOnly, SameSite Lax, and Secure when the base URL is HTTPS. All SQL uses placeholders; the two places that splice strings (`userBy` and `uniqueSlug`) take only internal constants or an allow-listed table name. Ownership is checked in the handler and again in the store for builds, photos, stash items, trophies, and friend actions. Private and hidden builds return the same 404 as a missing build. Photo and avatar file names are validated against a strict pattern before any path is built, so there is no traversal. Every upload is decoded and re-encoded so nothing a browser sent is stored or served as is (subject to finding 2). The Ko-fi token is compared in constant time and repeats are ignored by transaction id. Sending mail cannot inject headers: recipient addresses must parse as a bare address and subjects are fixed strings. The Discord post disables mentions. The Content-Security-Policy, nosniff, frame denial, and referrer policy are set on every response. Redirect targets are fixed paths or a same-host referer with double slash paths refused. The single SQLite connection is never held open across another query, so there is no self deadlock. Templates use html/template throughout and every user supplied value is in a text or attribute context that it escapes correctly. Config secrets never reach a template.
