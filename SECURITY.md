# Security policy

## Reporting a vulnerability

Report it privately, not as a public issue. Use GitHub's private
vulnerability reporting on this repository: open the Security tab and choose
"Report a vulnerability". That opens a thread only the maintainers can see.

Please include what you found, how to reproduce it, and what an attacker
could do with it. A proof of concept helps. Give the maintainers a chance to
fix it before you write about it anywhere public.

You should get a first reply within a week. This is a hobby project run by
one maintainer, so a fix takes as long as it takes, and you will be told
where it stands.

## What is worth reporting

Anything that lets somebody read or change what is not theirs: another
member's private builds, their stash, their journal, their friend list,
somebody else's session or sign-in link, a trophy STL download without
having won it, an admin action without being the admin. Also anything that
lets a visitor run script in another member's browser, forge a request on
their behalf, read files outside the data directory, or reach the database
through user input.

## What is not

A rate limit you found by hammering the sign-in form, a missing security
header with no exploitable consequence, output from an automated scanner with
nothing behind it, or anything that needs the server's own configuration
files or the operator's account to begin with.
