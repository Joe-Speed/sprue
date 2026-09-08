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

## 2. Email

Sign-in links go out by email. Nothing works without this. No domain needed.

1. Make a new Gmail account for the site, such as sprue.community@gmail.com. Personal account, not Workspace.
2. Signed in as that account, open https://myaccount.google.com/signinoptions/two-step-verification. Turn on 2-Step Verification.
3. Open https://myaccount.google.com/apppasswords. App name `sprue`. Create. Copy the 16 letters without spaces.

Write these down:

```
SPRUE_SMTP_HOST=smtp.gmail.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=sprue.community@gmail.com
SPRUE_SMTP_PASS=<the 16 letters>
SPRUE_SMTP_FROM=sprue.community@gmail.com
```

Gmail allows 500 emails a day. Brevo and the like need a domain, see step 10.

## 3. Railway

1. Go to railway.com. Sign in with GitHub.
2. Upgrade to the Hobby plan, 5 dollars a month. The Trial plan blocks outgoing email, so sign-in cannot work on it.
3. New Project, Deploy from GitHub repo, pick Joe-Speed/sprue. Railway finds the Dockerfile and builds. First build fails to start. That is expected, variables are missing.
4. Click the service. Settings tab.
5. Volume: on the project canvas, right click the service box, Attach Volume. Mount path `/data`.
6. Networking: Generate Domain. Copy the address, it ends in `.up.railway.app`. This is your site address until you buy a domain.
7. Health Check Path: `/healthz`.
8. Variables tab. Add these:

```
SPRUE_URL=https://<railway address>
SPRUE_ADMIN_EMAIL=<your login email>
SPRUE_SMTP_HOST=smtp.gmail.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=sprue.community@gmail.com
SPRUE_SMTP_PASS=<the 16 letters>
SPRUE_SMTP_FROM=sprue.community@gmail.com
```

9. Deploy. Wait for the green tick.
10. Open `https://<railway address>/healthz`. Expect the word `ok`.

End: site live on a Railway address.

Everywhere below, `https://<site>` means that Railway address. When you get a domain, it means the domain.

## 4. First sign-in

1. Open `https://<site>`. Sign in with the address you put in `SPRUE_ADMIN_EMAIL`.
2. Email arrives from sprue.community@gmail.com within a minute. Click the link.
3. Admin appears in the menu.
4. Post a build with a photo.
5. Railway, Deployments, Redeploy. Wait for green.
6. Reload the site. Build and photo still there. That proves the volume works.

End: admin account, volume proven.

## 5. Trophy files

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

## 6. Ko-fi

You have a Ko-fi account with Stripe connected.

1. Ko-fi, Settings, Payment. Set currency to GBP.
2. Ko-fi, More, Webhooks. Webhook URL: `https://<site>/webhooks/kofi`. Save. Copy the Verification Token.
3. Ko-fi, Your page. Copy the page address.
4. Railway, Variables. Add:

```
SPRUE_KOFI_URL=https://ko-fi.com/<yourpage>
SPRUE_KOFI_TOKEN=<verification token>
```

5. Redeploy. Wait for green.
6. Ko-fi Webhooks page, Send Test.
7. Open `https://<site>/support`. A gift from Jo Example is listed.
8. Open `https://<site>/admin`. Donations section. Press Remove on Jo Example.

End: Donate link in the footer, supporters list updates itself.

## 7. Discord, optional

1. Make a Discord server. Make an invite that never expires.
2. Make a channel called feedback. Channel settings, Integrations, Webhooks, New Webhook, Copy URL.
3. Railway, Variables:

```
SPRUE_DISCORD_URL=<invite link>
SPRUE_DISCORD_WEBHOOK=<webhook url>
SPRUE_SUPPORT_EMAIL=<your email>
```

4. Redeploy.

End: Discord, Feedback, and Support links in the footer.

## 8. Google, optional

Analytics:

1. analytics.google.com. Create a GA4 property for the site address. Copy the measurement ID, looks like G-XXXXXXXX.
2. Railway, Variables: `SPRUE_ANALYTICS_ID=G-XXXXXXXX`. Redeploy.

Search Console:

Needs your own domain, step 10.

1. search.google.com/search-console. Add property, Domain type, yourdomain.
2. Google shows a TXT record. Add it in Cloudflare, DNS. Press Verify.
3. Sitemaps. Submit `https://yourdomain/sitemap.xml`.

End: traffic numbers, Google indexes the site.

## 9. Photo screening, optional

1. console.cloud.google.com. New project. APIs and Services, Enable APIs, Cloud Vision API.
2. Credentials, Create credentials, API key. Edit the key, restrict it to Cloud Vision API.
3. Railway, Variables: `SPRUE_VISION_KEY=<key>`. Redeploy.

End: every photo checked before it is saved. First thousand a month free.

## 10. Your own domain, optional

Costs about 10 pounds a year. Do it when the site works on the Railway address.

1. Go to cloudflare.com. Make a free account.
2. Domain Registration, Register Domains. Buy one. Cloudflare sells at cost and sets up DNS for you.
3. Railway, service Settings, Networking, Custom Domain. Enter the domain. Railway shows a CNAME target.
4. Cloudflare, DNS, Records. Add record: type CNAME, name `@`, target the value Railway gave, proxy on (orange cloud).
5. Cloudflare, SSL/TLS, Overview. Set Full (strict).
6. Cloudflare, SSL/TLS, Edge Certificates. Turn on Always Use HTTPS.
7. Cloudflare, Speed, Optimization. Make sure Rocket Loader is off.
8. Railway, Variables. Change `SPRUE_URL` to `https://yourdomain`. Redeploy.
9. Ko-fi, Webhooks. Change the URL to `https://yourdomain/webhooks/kofi`.
10. Optional, email from the domain. Make a free Brevo account at brevo.com, 300 emails a day. Senders, Domains, add the domain, add the DNS records it shows in Cloudflare, Verify. SMTP and API, generate an SMTP key. Change the five SMTP variables in Railway:

```
SPRUE_SMTP_HOST=smtp-relay.brevo.com
SPRUE_SMTP_PORT=587
SPRUE_SMTP_USER=<login shown on Brevo's SMTP tab>
SPRUE_SMTP_PASS=<the SMTP key>
SPRUE_SMTP_FROM=hello@yourdomain
```

End: site at your domain, email from your domain.

## 11. Backups

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

## 12. Cost

Railway: free trial credit, then 5 dollars a month on Hobby. A domain about 10 pounds a year if you want one. Everything else free. Ko-fi covers Railway if a few members chip in.

## When something breaks

```sh
railway logs
```

MAINTAINING.md has the day to day details.
