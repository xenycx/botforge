# Deployment

## Requirements

* Linux with cgroup v2 and Docker Engine (or a rootless daemon on the **systemd**
  cgroup driver). The panel checks that memory, CPU and PID limits can be
  enforced and reports not-ready otherwise.
* Go >= 1.27 and Node >= 22 to build; neither is needed to run.

## Install

```sh
make build                 # npm ci, static UI build, embedded into bin/botpanel
sudo deploy/install.sh     # binary, directories, systemd units; creates no secrets
```

Then follow the printed steps: create an encryption key (`botpanel keygen`),
create the first admin (`botpanel create-admin EMAIL`, hidden password prompt
or `BOTPANEL_ADMIN_PASSWORD`), start `botpanel` and `botpanel-backup.timer`.
There is deliberately no unauthenticated first-run setup endpoint.

## Reverse proxy and TLS

The panel listens on loopback and sets `Secure` session cookies in production,
so it must be reached over HTTPS. The proxy must pass WebSocket upgrades on
`/api/v1/bots/*/console` and preserve `Host`/`Origin` (the console rejects
cross-origin handshakes).

```
# Caddy
panel.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Behind a proxy on the same host, set `BOTPANEL_PROXY_HEADER` (`X-Forwarded-For`
for Caddy/nginx, `CF-Connecting-IP` for Cloudflare) so rate limits apply per
client instead of per proxy. The header is trusted only from loopback peers, so
the proxy must run on the same machine; without it all clients share the
proxy's address. Cloudflare Tunnel and `panel.xenyc.ge` specifics are in
`oauth.md`.

## Configuration

See `deploy/systemd/botpanel.env.example`. Notable: `BOTPANEL_CONTAINER_USER`
(non-root uid:gid), `BOTPANEL_WORKSPACE_OWNER` (host uid:gid; differs under
rootless/userns), `BOTPANEL_CONTAINER_NETWORK`, `BOTPANEL_MAX_BOT_MEMORY_BYTES`,
`BOTPANEL_RUNTIMES_DIR` (override the six recipes, e.g. to pin digests or use a
registry mirror), `BOTPANEL_KEY_DIR`/`BOTPANEL_ACTIVE_KEY_ID`.

## Optional services and extra directories

* **OAuth sign-in and GitHub deployments:** `oauth.md`. Needs
  `BOTPANEL_PUBLIC_URL`; GitHub auto-deploy webhooks need GitHub to be able to
  reach `<PUBLIC_URL>/api/v1/webhooks/github`.
* **SFTP:** off by default; `BOTPANEL_SFTP_LISTEN=0.0.0.0:2022`. Open the port in
  your firewall. It is plain TCP: it does not work through an HTTP proxy or
  Cloudflare Tunnel. The host key is created at `/var/lib/botpanel/sftp_host_ed25519`.
* **Published bot ports:** off unless a bot opts in, within
  `BOTPANEL_PORT_RANGE` (default 20000-29999) on 127.0.0.1. Firewall the range
  from the internet unless you set `BOTPANEL_PORT_PUBLIC_BIND=1`.
* **Backups:** per-bot snapshots in `BOTPANEL_BACKUP_DIR`
  (`/var/lib/botpanel/backups`, covered by the service's `ReadWritePaths`).
  They are not part of `botpanel backup`; back that directory up too. Sealed
  values in them (and OAuth tokens in the database) need the same key files.
  `botpanel verify` checks environment variables, OAuth tokens, Discord webhooks
  and GitHub webhook secrets against the keys.

## Operations

* `/api/v1/healthz` liveness; `/api/v1/readyz` reports `database` and `docker`
  separately and returns 503 when either is down. Docker going away does not
  stop the panel or the bots; the runner reconnects and re-syncs.
* Restarting the panel does not restart bots: it adopts running containers.
* Logs: structured JSON on stderr (journald). Bot output is in Docker's rotated
  logs.
* Read `docs/isolation.md` before hosting untrusted users: there is no egress
  policy, admission control or disk quota out of the box.
