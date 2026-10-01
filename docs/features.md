# Feature guide

How each panel feature behaves, what it is bound by, and where it stops.
Setup for GitHub/Discord sign-in is in `oauth.md`; the automation API is in
`automation.md`.

## Interface

A single-page app embedded in the binary. Light and dark themes: the header
button (sun/moon/monitor) cycles Light → Dark → Match the system, and the
choice is remembered in the browser. Dark mode uses warm near-black surfaces
with an ember accent; every colour comes from theme tokens, so the terminal and
the code editor follow the theme too. **Settings → Appearance** also offers
twelve accent colours and four corner styles (Square, Subtle, Default,
Rounded); Square removes rounding everywhere, including badges and avatars.
These are per-browser preferences.

- **Fleet** (`/`): search, filters for state, runtime and owner, tags and
  favourites (all kept in the URL), a summary of running / needs attention /
  in progress / stopped bots, batch Start, Stop or Restart of up to 50 bots
  with a review step and a result per bot, and a Ctrl+K "Go to" palette that
  finds pages, administration sections, environment variables, docs, bots (and
  their sections), sites, people, AI chats and actions. Several words narrow
  the search; Tab switches scope; `>` lists actions only.
- **New bot** (`/bots/new`): template, GitHub repository or empty bot; required
  values such as the token up front; review of memory, CPU and build memory.
- **Bot page**: an identity card with shortcuts to the public page studio and
  resource settings, one row of tabs (Manage, Overview, Files, Deploy, Startup,
  Packages, Env, Network, Page, Health, Backups, Schedules, Access, Settings),
  and a status strip with the exact lifecycle state, live CPU, memory, disk and
  network readings, and Start, Restart, Stop and Kill. **Manage** is the
  default: a terminal window with the live console and a line that goes to the
  bot's standard input. A failed bot offers **Ask AI why**. Unsaved edits are
  protected when navigating away; older `?tab=console`, `?tab=ai` and
  `?tab=analytics` links still work.
- **Activity** (`/activity`): work (builds, deployments, backups, restores) and
  changes (who changed what; names only, never values).
- **Workspace switcher** (sidebar): scopes the overview, Sites and new bots to
  one workspace or shows all of them (`workspaces.md`).
- **Public page** (inside each bot): Page Studio for a generated bot site,
  opt-in public widgets, full site files, private insights and a cross-origin
  preview. **Sites** (`/sites`) remains the workspace-wide inventory for sites,
  releases, rollback and custom domains (`sites.md`).
- **Settings**: profile; appearance; workspaces and members; connected
  accounts; Security (password, two-step sign-in, sessions); SFTP keys and
  automation tokens. Each page explains a setting beside its controls on wide
  screens and above them on narrow ones.
- **Administration**: users (with each account's workspaces, bots and recent
  deployments), workspaces, sites and domains (suspend/restore), host
  resources, bots' resource use, capacity and panel logs, panel settings
  (sign-in, registration, the AI assistant), the environment editor, and
  diagnostics.

## Sharing and permissions (RBAC)

The owner (and administrators) can share a bot from **Access**, by email or
with an **invitation link**. Permissions are a bitmask:

| Permission | Allows |
| --- | --- |
| View console | live output, analytics, resource gauges, health |
| Start / stop | start, stop, restart, kill, console input |
| Edit files | file manager, SFTP, packages, deployments, backups (create/download) |
| Manage env vars | list, reveal, set and delete environment variables |
| Full admin | everything above plus startup, settings, telemetry key, GitHub link, alert rules, restore and delete backups |

Only the **owner or an administrator** can share the bot, delete it, transfer
it or publish host ports. A user with no grant gets `404`, indistinguishable
from a bot that does not exist; a user with a grant lacking a permission gets
`403`. Console input needs *Start / stop* and is re-checked on every line.
Console and SFTP sessions lose access within about 5 seconds of a revoked grant,
disabled account or deleted key.

Invitation links are one-use, expire after 1-14 days, carry the permissions
chosen when they were made, and only work for someone signed in to this panel.
The token lives in the URL fragment, which browsers never send to the server.
At most 20 open invitations per bot; they can be revoked.

Ownership transfer removes the GitHub link (it uses the old owner's GitHub
access) and moves the bot into the new owner's personal workspace. The last
administrator cannot be removed or disabled.

**Workspaces** grant access to every bot in them by role (owner, admin,
developer, viewer) on top of these per-bot grants; see
[workspaces.md](workspaces.md). Static websites are covered in
[sites.md](sites.md).

## Lifecycle, power controls and resource gauges

Start, Stop, Restart and **Kill** (immediate `SIGKILL`; records stopped intent
first). The API reports one `phase` per bot (starting, building, running,
retrying, failed, exited, stopping, stopped, ...), the restart count, the next
retry time and a reason code. Live CPU, memory and disk gauges relay Docker's
stats stream over Server-Sent Events. Disk is the size of the bot's workspace
(there are no per-bot disk quotas) next to free space on the node.

## Startup, runtime and auto-restart

**Startup** edits the runtime, the start command and an optional entrypoint.
Only programs approved for the runtime (`runtimes/*.yaml`) are accepted, and
the command runs directly, never through a shell.

Restart policy: `on_failure` (default) restarts after a non-zero exit with
exponential backoff and a limit on consecutive crashes (0 = unlimited); `never`
leaves the bot down. A clean exit is never restarted. When the runner gives up,
the bot shows *failed* with the reason, and **Start** retries with a fresh
crash budget. Backoff resets after a minute of stable running. Builds are
recorded with their output (256 KiB kept per build) and shown live.

## Environment variables

Values are masked in every list. **Reveal** fetches one value on request and
hides it again after 30 seconds. **Import .env** previews names before saving.
Names starting `BOTPANEL_` and `LD_`, and `PATH`, are reserved;
`BOTPANEL_TELEMETRY_KEY` and `BOTPANEL_URL` are set by the panel.

## Network and ports

Outbound access can be turned off (Docker's `none` network). Ports are **not
published by default**. The owner can publish host ports inside
`BOTPANEL_PORT_RANGE` (default 20000-29999), unique across bots, bound to
`127.0.0.1` unless `BOTPANEL_PORT_PUBLIC_BIND=1`.

**Bandwidth limits are recorded but not enforced.** Docker has no built-in
bandwidth cap; enforcing one needs traffic shaping (`tc`) on the host.

## Files

Web file manager with breadcrumbs, filter, upload with progress and cancel,
zip extraction and a CodeMirror editor (loaded on demand). Saves are
conditional on the file's revision: if SFTP, a deployment or another editor
changed the file, the editor offers to download your version, load theirs or
replace it deliberately, and a failed save keeps your text. While a deployment
or restore replaces files, edits wait with a clear 409.

Bot Sites use the same explorer and conflict-aware editor for a private site
draft. Unlike bot files, a save never changes production: **Publish draft**
creates and activates an immutable site release. Generated pages instead use
structured Page Studio settings plus bounded custom HTML and CSS.

## Package manager

Reads and edits `package.json`, `requirements.txt` (or `pyproject.toml`
`[project].dependencies`), `Cargo.toml` and `go.mod`. Search uses npm and
crates.io; PyPI and the Go proxy resolve an exact name. Names and version specs
are matched against per-ecosystem patterns, so nothing can be injected.
Dependencies install when the bot next starts. Java (Maven) and Ruby (Bundler)
manifests are not edited by the panel.

## Templates

Seven starters are embedded in the binary and copied into a new bot:

| Template | Runtime | Notes |
| --- | --- | --- |
| discord.js | Node.js 24 | CommonJS, BotForge SDK included |
| discord.js TypeScript | Node.js 24 | `src/index.mts` run directly by Node's built-in type stripping (no build step); `npm run check` type-checks locally |
| discord.py | Python | BotForge SDK included |
| Poise (Serenity) | Rust | builds in a 3 GiB build container |
| JDA | Java | Maven fetched and verified (pinned SHA-512) in the build container |
| DiscordGo | Go | |
| discordrb | Ruby 4 | `bundle install` into `vendor/bundle`; BotForge SDK included |

Each reads `DISCORD_TOKEN`, exits with a clear message when it is missing, and
registers `/ping`. Template metadata lists required variables, setup steps,
privileged intents and tested versions. A template may choose its start
command (the TypeScript starter runs `node src/index.mts`). Build containers get
their own memory floor from the runtime recipe (`build_memory_bytes`), used
only while building.

## GitHub deployments

Link a bot to a repository, branch and optional root directory. Requires GitHub
sign-in with repository access (`oauth.md`). The panel downloads the commit's
**tarball** through the GitHub API and unpacks it with the contained extractor:
no `git`, hooks or filters run on the host. **Deployments** shows the release,
"What changed?" (commits and files between the deployed commit and the branch),
and a history with redeploy and roll back to a chosen commit.

The file replacement is **crash-safe**: replaced files are moved aside, and a
journal outside the workspace records the plan before the first rename. If the
panel dies mid-way, the next start rolls the workspace back to the previous
files; the deployed commit is recorded before the previous files are dropped.
A Stop, Delete or Unlink that arrives during a deployment wins.

Auto-deploy uses a push webhook authenticated by HMAC-SHA256; unauthenticated
deliveries always get the same `401`. Limits: no submodules or Git LFS.

### Publishing a bot to GitHub

The other direction also works: **Deploy → Publish to GitHub** creates a new
repository (under your account or one of your organizations, private by
default) from the bot's current files, pushes them as the first commit and
links the bot to it, optionally with auto-deploy. For a linked bot, **Push to
GitHub** commits the current files (for example after editing them in the
panel or over SFTP) to the linked branch.

* Pushing uses the **acting user's own** GitHub connection with repository
  access (`repo` scope), never the token of whoever linked the repository, and
  needs *Edit files* on the bot (publishing a new repository needs *Full
  admin*).
* Nothing runs on the host: files become blobs, a tree and a commit through
  the GitHub Git Data API. Files GitHub already has are not uploaded again.
* The push is never forced. If the branch moved on GitHub meanwhile, the push
  stops with an explanation; deploy those changes first.
* With a linked sub-folder, only that folder of the repository is replaced;
  the rest of the repository stays.
* What is sent: every file except those excluded by `.gitignore` files (root
  and nested, with negation) and the built-in rules: `.git`, dependency and
  cache folders (`node_modules`, `.venv`, `venv`, `__pycache__`, `target` at
  the top, …), panel metadata, and `.env`/`.env.*` files (`.env.example`,
  `.env.sample`, `.env.template` and `.env.dist` are kept). Environment
  variables stored in the panel are never pushed. The dialog lists every file
  before anything is created, so secrets in other files can be spotted.
* Limits: 5,000 files, 200 MiB in total, 25 MiB per file (larger files are
  skipped and listed). Each push is recorded as a *Push to GitHub* operation
  in the deployment history; the pushed commit is recorded as deployed, and
  its own webhook delivery does not redeploy the bot.

## Backups

Manual and scheduled snapshots under `BOTPANEL_BACKUP_DIR` (mode 0600, SHA-256
recorded), with labels, integrity verification, a health summary and an
optional "stop the bot while copying" mode. Contents: the workspace without
rebuildable folders plus, optionally, the environment as **sealed** rows,
openable only with this server's key and the same bot ID.

Restore (full admin, bot stopped): one review dialog; verifies the checksum;
checks the environment decrypts **before changing anything**; takes a
`pre_restore` safety copy; unpacks into staging (rejecting `..`, absolute paths,
devices, hard links, escaping symlinks; dropping setuid bits); then replaces the
files with the same **journaled, crash-safe commit** as deployments. The
previous files are kept until the database records the restore, so a database
error or a crash returns the complete previous state.

Limits: 50 backups per bot (5 slots reserved for scheduled and safety copies),
2 GiB per workspace (`BOTPANEL_BACKUP_MAX_BYTES`). Backups, restores and
deployments refuse to start with less than `BOTPANEL_MIN_FREE_DISK_BYTES`
(default 1 GiB) free. They are **not** included in `botpanel backup`.

## Scheduled actions

**Schedules** runs backup, start, stop, restart or deploy on a timetable:
presets (daily, weekly, every few hours) or five-field cron, in any IANA time
zone, with the next runs previewed. Daylight-saving gaps are skipped and
repeated hours run once. One scheduler checks due rows every 30 seconds.

- Each schedule runs with its creator's **current** permissions; a removed
  grant or disabled account pauses it ("denied").
- If the panel was down at the due time, the run is recorded as **missed** and
  not repeated: no backlog after downtime.
- A scheduled restart never starts a stopped bot. Conflicts with running work
  are recorded as skipped.
- Per-bot backup schedules replace the panel-wide backup interval for that bot.
- At most 20 schedules per bot. "Run now" tests a schedule.

## Health and alerts

**Health & alerts** shows application health as reported by the bot itself,
separately from Docker's process state: every SDK push is a heartbeat and may
say whether the bot is connected to Discord (`ready`). A bot that never reported
is *unknown*, never *failed*.

An optional TCP or HTTP probe checks one of the bot's published TCP ports
through `127.0.0.1`. Intervals, timeouts, success/failure thresholds and startup
grace are configurable. When enabled, the panel replaces the container once on
an unhealthy transition; startup grace and transition-only restarts prevent a
broken deployment from flapping continuously. Probe targets cannot name an
arbitrary host, so this feature cannot be used for server-side request forgery.

Per-bot Discord notification preferences: crashes, deployments, backup
failures, recoveries, and an optional **heartbeat rule** that alerts once when a
running bot stops reporting for 1 minute to 1 hour (and once when it recovers).
A test message can be sent. Crash alerts are throttled to one per bot per 10
minutes.

## Host monitoring and panel logs

**Administration → Host** has four tabs.

- **Resources**: CPU, memory (cache, swap), load average, disk, network and disk
  throughput now and over 1 hour to 30 days (averages plus dashed peaks; the
  range is capped by `BOTPANEL_TELEMETRY_RETENTION`, 7 days by default), a
  disk-fill forecast from the trend, host facts, the BotForge process (memory,
  goroutines, open files, requests, errors, database size), the Docker runner
  and storage per location (directory sizes are counted in the background
  about every five minutes). A "needs attention" list flags a nearly full disk,
  memory or swap pressure, a high load, a Docker problem and recent errors.
- **Bots**: every bot's CPU (cores against its limit), memory against its
  limit, network totals since the container started, processes and workspace
  size; sortable, refreshed every 10 seconds, at most 60 running bots measured
  per refresh.
- **Capacity**: the admission budgets below, with each limit's current value
  and source.
- **Panel logs**: the newest 2,000 lines the panel logged since it started,
  with level filter, search, live follow, copy and download. Secrets, tokens and
  passwords are redacted before a line is kept. The view starts empty after a
  restart; the complete log stays in journald (`journalctl -u botpanel`) or
  `docker logs`.

## Environment variables from the panel

**Administration → Environment** lists every `BOTPANEL_` variable with its
description, default and source (default, environment file or set here) and
lets an administrator change most of them. Values are stored in the panel's
database (the metrics token encrypted) and layered over the process environment
when BotForge starts, so a change applies after a restart; the page shows what
is waiting. The environment file is never edited. Precedence: value set here,
then environment, then the built-in default; a value set here may be empty to
switch off something the environment enables.

Not editable here: variables read before the database opens (`BOTPANEL_ENV`,
`_LISTEN`, `_DB_PATH`, `_DB_MAX_CONNS`, `_DATA_ROOT`, `_KEY_DIR`,
`_ACTIVE_KEY_ID`, `_RUNTIMES_DIR`), ones that decide what the panel may do to
the host (`_DOCKER_HOST`, `_CONTAINER_USER`, `_WORKSPACE_OWNER`,
`_ALLOW_ROOT_CONTAINER_USER`, `_CONTAINER_NETWORK`, `_PROXY_HEADER`), and the
sign-in and address variables, which Panel settings owns. Every save is checked
as a whole configuration first; if a saved set ever stops validating, the panel
starts without it and says so on the page. **Restart panel** exits with code 75
and needs systemd (`Restart=on-failure`) or a container restart policy; without
a detected supervisor the button is hidden. Bot containers keep running through
a restart. From the host: `botpanel env` lists the saved values and `botpanel
env reset [NAME…]` drops them.

## Capacity and admission

Administrators can set budgets in the environment file or on the Environment
page (0 = unlimited):

| Setting | Effect |
| --- | --- |
| `BOTPANEL_NODE_MEMORY_BYTES` | sum of memory limits of bots wanted running; a start that would exceed it is refused (409) with the numbers |
| `BOTPANEL_USER_MEMORY_BYTES` | sum of a user's bot memory limits |
| `BOTPANEL_MAX_BOTS_PER_USER` | bots per user |
| `BOTPANEL_MAX_BUILDS` | concurrent build containers (default 1); waiting builds say so |
| `BOTPANEL_MIN_FREE_DISK_BYTES` | free space backups, restores and deployments need (default 1 GiB) |

The node check and the start are one database transaction, so simultaneous
starts cannot overcommit. Per-user budgets do not apply to administrators.
These are admission rules, not kernel limits: containers still have their own
hard memory limits, and there are no disk quotas.

## Accounts and sign-in

Passwords (Argon2id), GitHub and Discord sign-in (`oauth.md`), password change
that signs out other sessions, a sessions list with sign-out, and at most 20
sessions per user. `botpanel reset-password EMAIL` is the host recovery path.

**Two-step sign-in** (TOTP): set up in Security with any authenticator app
(key shown for manual entry, plus an `otpauth://` link for phones), confirmed
with a first code, with 10 one-use recovery codes (only hashes stored). It
applies after both password and provider sign-in. Codes cannot be replayed;
a sign-in ticket allows 5 attempts in 5 minutes. With two-step sign-in on, SFTP
no longer accepts the account password (use SFTP keys). `botpanel reset-mfa
EMAIL` removes it from the host.

## SFTP

Off unless `BOTPANEL_SFTP_LISTEN` is set. Sign in with your panel email plus
your password or an **SFTP key** (Settings → SFTP & API keys). One folder per
bot you may edit. Same containment as the web file manager. Access is re-checked
about every 5 seconds, including open file handles. No shell/exec/forwarding;
3 auth tries per connection, 10 failures per minute per address, 32 connections,
a per-file size cap. The host key is kept next to the database. **SFTP does not
work through Cloudflare's proxy or a Cloudflare Tunnel.**

## Automation API

Scoped bearer tokens (`bpa_...`) for scripts and CI: actions `read`, `power`,
`deploy`, `backup`, optionally limited to chosen bots, expiring after 1-366
days, revocable, with last use shown. Effective access is the token's scope
intersected with the owner's current permissions. `Idempotency-Key` makes
retries safe; every response has `X-Request-ID`; 120 requests per minute per
token. SFTP keys never work here and tokens never work for SFTP. See
`automation.md` and `/api/v1/automation/openapi.yaml`.

## Bot analytics

Generate a telemetry key in **Public page → Private insights** (stop the bot first; the key becomes
`BOTPANEL_TELEMETRY_KEY`). Bots push to `POST /api/v1/bot-telemetry` (or the
WebSocket at `/api/v1/bot-telemetry/ws`) with `Authorization: Bearer <key>`:

```json
{"ready":true,
 "stats":{"guilds":12,"members":3400,"ping":42},
 "commands":[{"name":"ban","count":2}],
 "events":[{"name":"guild_join","data":{"id":"1"}}]}
```

Limits: 16 KiB per push, 60 pushes per minute per bot, 16 stats / 20 commands /
10 events per push, event data up to 1 KiB of JSON, one sample per stat per 10
seconds, at most 20,000 rows per bot, 7 days retention. Snippets (served from
`/api/v1/sdk/<lang>`): discord.js, discord.py, Go (any library, standard
library only), Rust (reqwest), Java (JDK HTTP client) and Ruby (discordrb). They
push every 30 seconds, never queue failed pushes and never crash the bot.

## Discord notifications

If a user connects Discord with *Enable notifications*, alerts for their bots
are posted to the webhook they chose, following each bot's preferences (see
Health and alerts). Only Discord webhook URLs are ever called.

## Installation identity

Each installation has an identity stored in its database. Containers are
labelled with it, so two panels sharing one Docker daemon never list, adopt or
remove each other's containers. Containers created before the label existed are
adopted only when their bot exists in this database, and are never swept as
orphans. A restored installation keeps its identity. A lock file stops two
processes from using one database.

## Operations

`botpanel version | doctor | verify | reseal | backup | backup-verify | restore
| create-admin | reset-password | reset-mfa | keygen`. `reseal` (panel stopped)
re-encrypts every sealed value with the active key and can be re-run to
continue; keep old key files while per-bot backups made before the reseal exist.
`make release VERSION=x.y.z` builds Linux amd64/arm64 archives with
`SHA256SUMS`.
## AI assistant

One private AI chat opens from **Ask AI** on every page (bottom right, or the
sparkle button in the header). It sees which bot, site, section and file you
are looking at, can read a bot’s console output and build log, and can propose
changes that wait for your approval. See [AI operator](ai-operator.md) for
providers, permissions, approval modes, research, limits, data boundaries,
diagnostics and recovery behavior.
