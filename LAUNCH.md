# Going live

Do these in order. Each step says what you need before you start and what you have at the end.

## 1. Push the code

In the repo:

```sh
cd platform
gofmt -l .
go vet ./...
go test ./...
cd ..
git add -A
git commit -m "Ko-fi donations and support page"
git push
```

End: GitHub has the current code.

## 2. Domain

You need a domain you own.

1. Go to cloudflare.com. Make a free account.
2. Add your domain as a site. Pick the Free plan.
3. Cloudflare shows two nameservers. Set them at your registrar.
4. Wait until Cloudflare says the site is active. Can take an hour.

End: Cloudflare controls DNS for your domain.

## 3. Email

Sign-in links go out by email. Nothing works without this.

1. Go to brevo.com. Make a free account.
2. Senders, Domains and Dedicated IPs. Add a sender such as hello@yourdomain. Click the link Brevo emails you.
3. Domains. Add yourdomain. Brevo shows three DNS records. Add each one in Cloudflare, DNS, Records. Come back and press Verify.
4. SMTP and API, SMTP tab. Generate SMTP key. Copy it.

Write these down:

```
SPRUE_SMTP_HOST=smtp-relay.brevo.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=<login shown on the SMTP tab>
SPRUE_SMTP_PASS=<the SMTP key>
SPRUE_SMTP_FROM=hello@yourdomain
```

End: five SMTP values.

## 4. Railway

1. Go to railway.com. Sign in with GitHub.
2. New Project, Deploy from GitHub repo, pick Joe-Speed/sprue. Railway finds the Dockerfile and builds. First build fails to start. That is expected, variables are missing.
3. Click the service. Settings tab.
4. Volumes: Add Volume. Mount path `/data`.
5. Networking: Generate Domain. Note the address, it ends in `.up.railway.app`.
6. Health Check Path: `/healthz`.
7. Variables tab. Add these:

```
SPRUE_URL=https://yourdomain
SPRUE_ADMIN_EMAIL=you@yourdomain
SPRUE_SMTP_HOST=smtp-relay.brevo.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=...
SPRUE_SMTP_PASS=...
SPRUE_SMTP_FROM=...
```

8. Deploy. Wait for the green tick.
9. Open `https://<railway address>/healthz`. Expect the word `ok`.

End: site running on a Railway address.

## 5. Point the domain at Railway

1. Railway, service Settings, Networking, Custom Domain. Enter yourdomain. Railway shows a CNAME target.
2. Cloudflare, DNS, Records. Add record: type CNAME, name `@`, target the value Railway gave, proxy on (orange cloud).
3. Cloudflare, SSL/TLS, Overview. Set Full (strict).
4. Cloudflare, SSL/TLS, Edge Certificates. Turn on Always Use HTTPS.
5. Cloudflare, Speed, Optimization. Make sure Rocket Loader is off.
6. Wait a few minutes. Open `https://yourdomain/healthz`. Expect `ok`.

End: site at your domain.

## 6. First sign-in

1. Open `https://yourdomain`. Sign in with the address you put in `SPRUE_ADMIN_EMAIL`.
2. Email arrives from Brevo within a minute. Click the link.
3. Admin appears in the menu.
4. Post a build with a photo.
5. Railway, Deployments, Redeploy. Wait for green.
6. Reload the site. Build and photo still there. That proves the volume works.

End: admin account, volume proven.

## 7. Trophy files

You need first-place.stl, second-place.stl, third-place.stl. They are never in the repo.

1. Install the Railway CLI:

```sh
npm install -g @railway/cli
railway login
cd ~/Code-Projects/sprue
railway link
```

2. Upload the files. Put them somewhere with a temporary link first, such as a private Dropbox or Drive share link, then:

```sh
railway ssh
mkdir -p /data/stl
curl -L -o /data/stl/first-place.stl "<link>"
curl -L -o /data/stl/second-place.stl "<link>"
curl -L -o /data/stl/third-place.stl "<link>"
ls -l /data/stl
exit
```

Names must match exactly.

End: winners can download trophies.

## 8. Ko-fi

You have a Ko-fi account with Stripe connected.

1. Ko-fi, Settings, Payment. Set currency to GBP.
2. Ko-fi, More, Webhooks. Webhook URL: `https://yourdomain/webhooks/kofi`. Save. Copy the Verification Token.
3. Ko-fi, Your page. Copy the page address.
4. Railway, Variables. Add:

```
SPRUE_KOFI_URL=https://ko-fi.com/<yourpage>
SPRUE_KOFI_TOKEN=<verification token>
```

5. Redeploy. Wait for green.
6. Ko-fi Webhooks page, Send Test.
7. Open `https://yourdomain/support`. A gift from Jo Example is listed.
8. Open `https://yourdomain/admin`. Donations section. Press Remove on Jo Example.

End: Donate link in the footer, supporters list updates itself.

## 9. Discord, optional

1. Make a Discord server. Make an invite that never expires.
2. Make a channel called feedback. Channel settings, Integrations, Webhooks, New Webhook, Copy URL.
3. Railway, Variables:

```
SPRUE_DISCORD_URL=<invite link>
SPRUE_DISCORD_WEBHOOK=<webhook url>
SPRUE_SUPPORT_EMAIL=you@yourdomain
```

4. Redeploy.

End: Discord, Feedback, and Support links in the footer.

## 10. Google, optional

Analytics:

1. analytics.google.com. Create a GA4 property for yourdomain. Copy the measurement ID, looks like G-XXXXXXXX.
2. Railway, Variables: `SPRUE_ANALYTICS_ID=G-XXXXXXXX`. Redeploy.

Search Console:

1. search.google.com/search-console. Add property, Domain type, yourdomain.
2. Google shows a TXT record. Add it in Cloudflare, DNS. Press Verify.
3. Sitemaps. Submit `https://yourdomain/sitemap.xml`.

End: traffic numbers, Google indexes the site.

## 11. Photo screening, optional

1. console.cloud.google.com. New project. APIs and Services, Enable APIs, Cloud Vision API.
2. Credentials, Create credentials, API key. Edit the key, restrict it to Cloud Vision API.
3. Railway, Variables: `SPRUE_VISION_KEY=<key>`. Redeploy.

End: every photo checked before it is saved. First thousand a month free.

## 12. Backups

Once a month:

```sh
railway ssh
apk add sqlite
sqlite3 /data/sprue.db ".backup /data/backup.db"
exit
```

Then copy `/data/backup.db` and `/data/photos` off the server. Railway has no download button, so from your machine:

```sh
railway ssh -- tar cz /data/backup.db /data/photos > sprue-backup-$(date +%F).tgz
```

End: a copy you can restore by putting the files back on the volume.

## 13. Cost

Railway: free trial credit, then 5 dollars a month on Hobby. Everything else free. Ko-fi covers Railway if a few members chip in.

## When something breaks

```sh
railway logs
```

MAINTAINING.md has the day to day details.
