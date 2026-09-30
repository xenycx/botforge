# Static site hosting

BotForge can host static websites next to your bots: a bot's dashboard, its
documentation, a landing page or an invite page. A site is a set of files
(HTML, CSS, JavaScript, images) served exactly as uploaded. There is no
server-side code and no build step on the server: build locally or in CI and
publish the output.

## How it is served (and why separately)

Sites are served by a **separate listener** (`BOTPANEL_SITES_LISTEN`) that
never serves the panel, and always on **different host names** from the panel.
Site files are arbitrary user HTML and scripts; on the panel's origin they
could read CSRF tokens, register service workers or act as the signed-in
user. On their own host names they cannot.

* Default address: `<slug>.<sites domain>`, for example
  `https://docs.sites.example.com`, from `BOTPANEL_SITES_BASE_URL`.
* Custom domains: any number of verified host names per site (up to 10).
* The listener maps the request's `Host` to the site's current **release** and
  serves files from that directory through a descriptor-contained root: paths
  cannot leave the release, and symlinks cannot point outside it.
* Dot-files and dot-directories (`.env`, `.git/`, …) are never served, even if
  uploaded (`.well-known/` is the exception).
* `GET` and `HEAD` only. HTML is sent with `Cache-Control: no-cache` so a new
  release shows immediately; other files are cacheable for an hour.
  Conditional and range requests are supported.
* Options per site: **single-page application** (unknown paths serve
  `/index.html`) and **clean URLs** (`/about` serves `about.html` or
  `about/index.html`; on by default). A `404.html` at the top level is used for
  missing pages.

Recommendation: use a **separate registrable domain** for sites (like
`github.io` vs `github.com`), for example `example-sites.net`. BotForge
refuses configurations where the panel's host would be a site host name, and
in production refuses a panel host equal to the sites domain. A sites domain
that merely shares a parent with the panel (panel `panel.example.com`, sites
`sites.example.com`) works, but sites could then set cookies for
`example.com`.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `BOTPANEL_SITES_LISTEN` | off (development: `127.0.0.1:8081`) | address of the sites listener; empty disables hosting |
| `BOTPANEL_SITES_BASE_URL` | development: `http://localhost:8081` | origin whose host is the sites domain, e.g. `https://sites.example.com` |
| `BOTPANEL_SITES_DIR` | `/var/lib/botpanel/sites` | releases, one directory per site |
| `BOTPANEL_SITE_MAX_BYTES` | 104857600 (100 MiB) | largest release, uncompressed (1 MiB–4 GiB) |
| `BOTPANEL_MAX_SITES_PER_USER` | 10 | sites an account may create; 0 = unlimited; administrators are exempt |
| `BOTPANEL_SITES_DNS_TARGET` | the site's default host | host name shown as the CNAME target for custom domains |

ZIP uploads are also bounded by `BOTPANEL_MAX_UPLOAD_BYTES` (compressed size).
Five releases are kept per site (the serving one is never pruned).

In development (`BOTPANEL_ENV=development`) hosting is on by default and sites
are reachable at `http://<slug>.localhost:8081` (browsers resolve
`*.localhost` to the local machine).

## DNS and reverse proxy

1. Create a wildcard DNS record `*.sites.example.com` pointing at the server.
2. Terminate TLS in a reverse proxy on the same host and forward site traffic
   to `BOTPANEL_SITES_LISTEN`. Keep forwarding the panel host to
   `BOTPANEL_LISTEN` as before.

With **Caddy**, on-demand TLS obtains certificates for the wildcard subdomains
and for verified custom domains as visitors arrive. The sites listener answers
Caddy's permission check at `/.well-known/botforge/tls-allowed?domain=…`
(loopback peers only), so certificates are only requested for host names a
live site answers on:

```
{
    on_demand_tls {
        ask http://127.0.0.1:8081/.well-known/botforge/tls-allowed
    }
}

panel.example.com {
    reverse_proxy 127.0.0.1:8080
}

# Every site subdomain and every verified custom domain.
https:// {
    tls {
        on_demand
    }
    reverse_proxy 127.0.0.1:8081
}
```

With nginx or another proxy, use a wildcard certificate for
`*.sites.example.com` (DNS challenge) and add certificates for custom domains
yourself; proxy everything that is not the panel host to the sites listener
with the original `Host` header.

Enforcement boundary: TLS is terminated and certificates are issued by the
reverse proxy. BotForge only answers whether a host name is allowed.

## Custom domains

1. Add the domain on the site's page (for example `www.example.com`).
   Internationalized names are stored in punycode.
2. Create the two records it shows at your DNS provider:
   * `TXT _botforge-verify.www.example.com` with the value
     `botforge-verify=<token>`: proves you control the domain.
   * `CNAME www.example.com` → the site's default host (or
     `BOTPANEL_SITES_DNS_TARGET`), or an `A`/`AAAA` record with the server's
     address. Root domains need an ALIAS/ANAME or `A` record.
3. Press **Check DNS now**. The domain is served only after the TXT record
   matches.

Rules: a domain verified for one site cannot be added to another; an
unverified claim does not reserve a domain (someone who verifies first wins).
The panel's own host and its subdomains, and names under the sites domain, are
refused. Verified domains are re-checked every six hours; a failed re-check is
reported on the site page but does not stop serving (DNS hiccups must not take
sites down). Remove the domain to stop serving it.

## Publishing

* **ZIP upload** (drag and drop on the site page, or
  `POST /api/v1/sites/{id}/upload` with the archive as the body). A single
  top-level folder such as `dist/` is unwrapped. The archive is validated and
  extracted with the same hardened extractor as bot uploads (no links, no
  special files, size and entry limits); a rejected archive changes nothing.
* **GitHub**: link a repository, branch and optional folder on the site page,
  then **Deploy latest commit**. The branch should contain built files (for
  example `gh-pages`, or a `dist` folder committed by CI). Public repositories
  work without a connected account; private ones use the GitHub access of the
  member who linked the repository. The tarball is downloaded through the API,
  as for bots; nothing from the repository runs.
* **Rollback**: every publish is a new immutable release; **Serve this** on an
  earlier release switches back instantly.

## Access and administration

Sites belong to a [workspace](workspaces.md): viewers can look, developers can
publish, deploy, roll back and manage domains, admins can also delete and move
sites. Panel administrators see every site under **Administration → Sites and
domains** and can **suspend** a site: visitors then get an "unavailable" page
on every address until it is restored; nothing is deleted.

## What it does not do

No server-side code, PHP, redirects/headers configuration files, form handling,
password protection, bandwidth limits, per-site analytics or build pipelines.
Uploaded content is not scanned for malware. These are listed in
[implementation-status.md](implementation-status.md).

## API

| Method and path | Purpose |
| --- | --- |
| `GET /sites-info` | whether hosting is on and how addresses look |
| `GET /sites`, `POST /sites` `{name, slug?, workspace_id?, spa?}` | list and create |
| `GET /sites/{id}` | site, role, domains with DNS records, releases, GitHub deploy state |
| `PATCH /sites/{id}` `{name?, spa?, clean_urls?, workspace_id?, repo?}` | settings; `repo` is `{full_name, branch, root_dir}` or `{clear: true}` |
| `DELETE /sites/{id}` | delete with every release and domain (workspace admin) |
| `POST /sites/{id}/upload` | publish a ZIP (raw body) |
| `POST /sites/{id}/deploy` | deploy the linked branch head (background) |
| `POST /sites/{id}/releases/{release}/activate` | serve an earlier release |
| `POST /sites/{id}/domains` `{domain}` | add a custom domain |
| `POST /sites/{id}/domains/{domain}/verify` | check its TXT record now |
| `DELETE /sites/{id}/domains/{domain}` | remove it |
| `GET /admin/sites`, `PATCH /admin/sites/{id}` `{disabled}` | administrators: list, suspend or restore |
