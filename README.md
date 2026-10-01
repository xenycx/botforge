# BotForge

Current version: **0.2.0**. See [CHANGELOG.md](CHANGELOG.md) for release notes
and [the implementation status](docs/implementation-status.md) for the staged
platform-overhaul checklist.

A lightweight hosting panel for Discord bots written in Node.js, Python, Rust,
Go, Java or Ruby. Go + Fiber backend, SQLite, SvelteKit UI embedded in one
static binary, Docker for isolation.

* Bot lifecycle (start / stop / restart / delete) driven by persisted desired
  state and a reconciler, safe across crashes and lost Docker responses
* Resource-capped, hardened containers (memory, CPU, PIDs, read-only root,
  no capabilities, non-root); separate build stage per language
* Live stdout/stderr and controlled stdin over WebSocket, reconnect/resume,
  bounded slow-client handling
* Web file manager (CodeMirror), zip upload with safe extraction
* AES-256-GCM encrypted environment variables, masked in every response
* Node telemetry with bounded retention; SQLite-aware backup and restore
* Users with ownership checks, Argon2id, CSRF-protected cookie sessions
* Sign-in with GitHub and Discord (linkable, self-hostable OAuth; `docs/oauth.md`)
* Optional email through Mailgun: password reset, invitations, bot alerts and security notices (`docs/email.md`) and an admin Announcements tab for HTML news and policy updates; off by default and free at idle
* Quickstart templates (discord.js, discord.py, Poise, JDA, DiscordGo) and
  GitHub deployments from **any public repository** (no GitHub connection
  needed) with auto-deploy on push or by polling
* **Analyze repository**: detects language, start/build commands, variables,
  databases and resources, with verified recipes for Red-DiscordBot and YAGPDB
  and optional AI refinement
* **Add-ons**: PostgreSQL, Redis, MongoDB and MariaDB per bot on a private,
  internet-less network, with connection variables injected; custom build
  commands for larger projects
* Custom **logos** for bots and sites, Discord avatars fetched with the bot's
  token, site favicons picked up automatically
* Power controls incl. emergency kill, live CPU/RAM/disk gauges, per-bot
  auto-restart policy with backoff, startup/entrypoint editor, network and
  published-port controls
* Team **workspaces** with owner/admin/developer/viewer roles on top of per-bot
  sharing, and administrator views of every workspace, account and deployment
  (`docs/workspaces.md`)
* **Static site hosting** on a separate listener: ZIP or GitHub publishing,
  instant rollback, editable site addresses, several sites domains (added at
  runtime with DNS verification), custom domains with DNS verification,
  on-demand TLS via the reverse proxy (`docs/sites.md`)
* Integrated **Bot Sites** in every Discord bot: a generated public page with
  custom HTML/CSS and opt-in live widgets, or full HTML/CSS/JS files edited in
  a private draft and published as immutable releases
* **Publish bots to GitHub**: create a repository from a bot's files and push
  later changes, `.gitignore`-aware and never force-pushed
* Sub-user sharing with granular permissions; embedded SFTP server with API keys
* Visual package manager (npm, pip, Cargo, Go modules), per-bot backups with
  restore, bot-to-panel analytics API with discord.js / discord.py snippets,
  Discord notifications (`docs/features.md`)

Architecture, deployment, API, and feature guides are collected in [`docs/`](docs/README.md).
Phase 1 is a **single-host** platform: no multi-host scheduling, HA, billing,
interactive shell, or standalone runner daemon (`cmd/botrunner`).

## Build and run

```sh
make build      # npm ci + static UI build -> internal/webui/dist -> bin/botpanel
make run-dev    # BOTPANEL_ENV=development: data under ./.dev-data, dev key auto-created
make check      # go vet, go test, svelte-check
make integration  # real-Docker tests, needs a DISPOSABLE daemon (see the Makefile)
```

Requires Go >= 1.27 and Node >= 22 to build (module path is the placeholder
`botpanel`). Production install: `docs/deployment.md`. First run:

```sh
botpanel keygen                          # encryption key (kept outside the database)
botpanel create-admin you@example.com    # hidden password prompt
botpanel                                 # serve on BOTPANEL_LISTEN (default 127.0.0.1:8080)
```

Other commands: `backup`, `backup-verify`, `restore`, `verify` (`docs/backup.md`).

### Run the published container

```sh
docker pull ghcr.io/xenycx/botforge:latest
docker compose up -d
```

See [the container guide](docs/container.md) before mounting the Docker socket.

## Documentation

Start at the [documentation index](docs/README.md). The project home is
[github.com/xenycx/botforge](https://github.com/xenycx/botforge).

* `docs/architecture.md`: state model, reconciliation, console semantics
* `docs/isolation.md`: what containers get, the evidence, and **what is not provided**
* `docs/backup.md`, `docs/deployment.md`, `docs/footprint.md`
* `docs/features.md`: every panel feature, its limits and what it does not do
* `docs/workspaces.md`: team workspaces, roles and administrator oversight
* `docs/sites.md`: static site hosting, custom domains, DNS and reverse proxy setup
* [CHANGELOG.md](CHANGELOG.md): versioned release history
* [docs/implementation-status.md](docs/implementation-status.md): completed and remaining platform-overhaul work
* `docs/oauth.md`: GitHub/Discord sign-in setup, Cloudflare, troubleshooting
* `docs/email.md`: Mailgun setup, what is emailed, security properties and limits

## Validation (2026-09-30, Linux 7.2.5 x86_64, Go 1.27.1, Node 26.8.1)

| Check | Result |
| --- | --- |
| `go vet ./...` (also `-tags integration`), `go test ./...`, `go test -race ./internal/...` | pass |
| `svelte-check` | 0 errors (21 `state_referenced_locally` warnings, all in components keyed by a route param) |
| Real-Docker integration tests (`make integration`), all 15 | pass on a fresh **rootful** Docker 29.7.2 daemon (cgroup v2, systemd driver): all six language runtimes, lifecycle, isolation settings, crash restart, adoption after a panel restart, OOM, build failure, backup/restore cycle, console/stdin/reconnect/slow client, and the new SIGKILL, restart-policy (clean exit / gave up / never), published-port (answered over HTTP), custom-entrypoint, no-network and stats-stream tests |
| **Every template built and run through the panel binary in real containers**, with an invalid token | discord.js (`TokenInvalid`), discord.py (`LoginFailure`), DiscordGo (`4004 Authentication failed`), JDA (Maven downloaded and verified, `InvalidTokenException`), Poise (`Sent invalid authentication`): each reached Discord's login |
| Real browser (Playwright, headless Chromium), 30 checks | pass: OAuth buttons on the login page, template gallery, every bot tab, env reveal/hide/import, package search and add, startup/network/backup saves, telemetry key and SDK snippet, connected accounts, SFTP page and API key, no horizontal scroll at phone width, no unexpected console errors |
| Real OpenSSH `sftp` client against the production binary | list, put, mkdir, rename, get, rm, rmdir work; root mkdir, deleting a bot folder and `../..` traversal are refused; a wrong password is rejected; `ssh host cmd` is refused |
| Idle RSS, panel + in-process runner, everything enabled | **28,344,320 bytes** (target < 50 MB); peak 46.3 MiB during an Argon2id login; `docs/footprint.md` |

**Not verified against the real services** (no credentials or accounts were
available): the live GitHub and Discord OAuth exchanges, GitHub's webhook
delivery and API, and Discord webhook posting. They are tested against fake
provider servers that reproduce the documented request and response shapes
(including GitHub's HTTP 200 error bodies and codeload redirects). The first
real login is the moment to check `docs/oauth.md`.

The Ruby runtime has no template (its smoke test passes). Bandwidth limits are
not enforced (Docker has no native cap).

## Known limitations

* **Egress, admission control and disk quotas are not enforced by BotForge**;
  see `docs/isolation.md` before hosting untrusted users. The firewall example
  there was not run in the test environment.
* The panel holds the Docker socket (root-equivalent). Its systemd sandbox
  limits blast radius but does not isolate that privilege.
* Not exercised: the default non-root container uid (65532, needs CAP_CHOWN; the
  tests ran the container as the test user's own uid on a rootful daemon, or as
  root under the earlier rootless one), other CPU architectures, glibc images,
  and load (many bots / connections).
* Dependency installs need outbound network in the build container; the template
  runs exercised real registries (npm, PyPI, Go proxy, crates.io, Maven Central).
* Builds need real RAM: the Rust recipe reserves 3 GiB for its build container
  (`build_memory_bytes`), because `rustc` is SIGKILLed at 1.5 GiB without swap.
* `npm audit` reports low-severity findings in dev dependencies (not triaged).
* Recorded in `docs/architecture.md`: console log delivery is at-least-once at
  reconnect boundaries, not exactly-once.
* One panel-wide **AI assistant** (the Ask AI button on every page) that knows
  which bot, site and section you are viewing: streamed investigation that
  reads console output, build logs and files, approval-gated or bounded
  automatic repair (file changes, offline diagnostics and restarts under
  per-run limits, all audited), encrypted OpenAI-compatible providers
  configurable in `/setup` or Administration, cited web research, and staged
  diffs with undo.
* Administrator tools: a Host page with live and historical CPU, memory, load,
  disk and network charts, per-bot resource use, storage, Docker and process
  details and the panel's own log; an Environment page that edits `BOTPANEL_`
  variables from the browser (applied at the next restart); and a Ctrl+K "Go to"
  palette that finds pages, settings, variables, bots, sites, people, chats and
  actions. It cannot change startup commands, deploy, publish sites or
  push to GitHub.
