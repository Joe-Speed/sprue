# Going live

Everything needed to take sprue from this repository to a public site, in order. Nothing here costs money except Railway after its trial credit and a domain if you do not already own one.

## 1. Accounts

Create these before touching the code. Each takes a few minutes.

- Railway, at railway.com, to run the binary and hold the data volume.
- Cloudflare, at cloudflare.com, for DNS, HTTPS, and caching in front of Railway. Add your domain as a site on the free plan and move its nameservers to Cloudflare.
- Brevo, at brevo.com, for the sign-in emails and stash reminders. The free plan sends 300 a day over SMTP.
- Google Analytics, at analytics.google.com, if you want traffic numbers. Create a GA4 property for the domain and note its measurement ID, which looks like G-XXXXXXXX.
- Google Search Console, at search.google.com/search-console, so Google indexes the site and shows you what it finds.

## 2. Email

Any SMTP provider works; the five variables are the same. Brevo is the suggestion because its free plan needs no domain to start. Resend (about 3,000 a month free, needs your own domain), Postmark (100 a month free, the best deliverability), Mailjet (200 a day free), and Amazon SES (cheap, needs an AWS account and production access) all fit the same way. Change provider later by changing the variables.

In Brevo, open Senders and add the address mail will come from, for example hello@yourdomain. Verify it by clicking the link Brevo sends. Then open SMTP and API, create an SMTP key, and note the four values:

```
SPRUE_SMTP_HOST=smtp-relay.brevo.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=<the login email shown on the SMTP page>
SPRUE_SMTP_PASS=<the SMTP key, not your Brevo password>
SPRUE_SMTP_FROM=<the verified sender address>
```

Deliverability improves a lot if the domain is authenticated: Brevo's Domains page gives you three DNS records to add in Cloudflare. Do it once and mail stops landing in spam.

## 3. Railway

Create a new project from the GitHub repository. Railway reads the Dockerfile at the root and builds the image.

Storage: add a volume to the service and mount it at `/data`. This is where the database, photos, and trophy files live. Without it everything is lost on each deploy.

Variables, on the service:

```
SPRUE_URL=https://yourdomain
SPRUE_ADMIN_EMAIL=you@yourdomain
SPRUE_SMTP_HOST=smtp-relay.brevo.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=...
SPRUE_SMTP_PASS=...
SPRUE_SMTP_FROM=...
SPRUE_ANALYTICS_ID=G-XXXXXXXX        optional
SPRUE_SITE_VERIFICATION=...          optional, see step 6
SPRUE_VISION_KEY=...                  optional, see step 8
SPRUE_DISCORD_URL=...                 optional, see step 7
SPRUE_DISCORD_WEBHOOK=...             optional, see step 7
SPRUE_SUPPORT_EMAIL=...               optional
SPRUE_CURRENCY=£                      optional, default £
```

`PORT` and `SPRUE_DATA` are already handled: Railway sets `PORT`, the Dockerfile sets `SPRUE_DATA=/data`. The binary refuses to start with `SPRUE_URL` set to a real domain and no SMTP host, so the sign-in flow cannot silently break.

Networking: in the service settings, generate a Railway domain first to check the deploy works, then add your custom domain. Railway shows a CNAME target to use at Cloudflare.

Health check: set the path to `/healthz`. Railway then restarts the service if the database ever stops answering.

## 4. Cloudflare

DNS: add a CNAME for your domain, or for `www`, pointing at the target Railway gave you, with the proxy switched on (orange cloud). If you use the bare domain, Cloudflare flattens the CNAME for you.

SSL/TLS: set the mode to Full (strict). Railway serves HTTPS itself, so this end-to-end setting is right. Turn on Always Use HTTPS under Edge Certificates.

Caching: nothing to configure. Static files and photos already carry cache headers and Cloudflare honours them. Pages are served fresh.

Rules worth adding on the free plan: none are required. Leave Rocket Loader, Auto Minify, and Email Obfuscation off. Rocket Loader in particular injects a script the Content-Security-Policy would block.

## 5. Trophy files

Copy first-place.stl, second-place.stl, and third-place.stl into `/data/stl/` on the volume. Railway has no file upload in the dashboard, so use the CLI:

```sh
npm install -g @railway/cli
railway login
railway link
railway ssh
```

Inside the shell, fetch the files from wherever you keep them privately, for example a temporary signed link, and place them:

```sh
mkdir -p /data/stl
curl -o /data/stl/first-place.stl "<private url>"
curl -o /data/stl/second-place.stl "<private url>"
curl -o /data/stl/third-place.stl "<private url>"
```

The names must match exactly. TROPHIES.md covers designing them.

## 6. Google

Analytics: paste the measurement ID into `SPRUE_ANALYTICS_ID` and redeploy. Without the variable the site serves only its own small site script; with it, Google's tag is allowed too and nothing else. Real-time reports in GA4 show your own visit within a minute.

Search Console: add the domain as a property. The DNS method is simplest: Google gives you a TXT record, you add it in Cloudflare, done. The alternative is the HTML tag method: put the content value into `SPRUE_SITE_VERIFICATION` and redeploy. Once verified, open Sitemaps and submit `https://yourdomain/sitemap.xml`. The sitemap is built from the database on every request, so it never needs resubmitting.

## 7. Discord and feedback

Create a Discord server for the community and make an invite link that does not expire. Put it in `SPRUE_DISCORD_URL` and it appears in the footer.

For feedback, make a channel for it, open its settings, Integrations, Webhooks, create one, and copy its URL into `SPRUE_DISCORD_WEBHOOK`. Signed-in members then get a feedback page, and each message lands in that channel with their name and a link to their profile. Mentions are switched off in the post so nobody can ping the server through it, and one message a minute per member is the limit. Treat the webhook URL as a secret; anyone holding it can post to the channel.

Set `SPRUE_SUPPORT_EMAIL` to an address you read and a support link joins the footer.

## 8. Photo screening

Members can report any build and you can hide it from the admin page, which is the part that matters for a small site. Automatic screening on top of that is optional and uses Google Cloud Vision's SafeSearch: the first thousand images a month are free, then about a dollar fifty per thousand.

In Google Cloud console, create a project, enable the Cloud Vision API, then under Credentials create an API key and restrict it to the Cloud Vision API only. Put it in `SPRUE_VISION_KEY` and redeploy. Every uploaded photo is then checked before it is saved; anything rated likely adult or violent is refused with a plain message, and if the check cannot be reached the photo is refused rather than let through. Without the key nothing is checked and nothing changes.

## 9. First run

Open `https://yourdomain/healthz` and expect `ok`.

Sign in with the address in `SPRUE_ADMIN_EMAIL`. The email should arrive from Brevo within a minute; that account becomes the admin.

Post a build with a photo from the community page. Then trigger a redeploy in Railway and check the photo and your account are still there. This proves the volume is mounted and used.

Start a competition, enter the build, and check the competition page. Voting opens the day after the entry date and results follow the day after the voting date, both automatically.

Add a kit to your stash, set a goal, and switch reminders to weekly in settings. The first reminder arrives a week later.

## 10. Backups

Railway volumes are durable but not versioned on the free and Hobby plans. Take your own copies. From `railway ssh`:

```sh
apk add sqlite
sqlite3 /data/sprue.db ".backup /data/backup.db"
```

Then copy `/data/backup.db` and the `/data/photos` folder somewhere else. Doing this monthly by hand is fine at first. The database is one file and photos are plain JPEGs, so restoring means putting them back on the volume.

## 11. Money

Railway: free trial credit, then the Hobby plan at five dollars a month. Cloudflare, Brevo, Google Analytics, Search Console, and Vision at under a thousand photos a month: free. A domain is the only other cost if you need to buy one.

## Afterwards

When something goes wrong, `railway logs` shows the request log and any errors. MAINTAINING.md covers day to day operation: the competition lifecycle, trophy re-arming, and the environment variables.
