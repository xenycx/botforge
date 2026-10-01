# Platform overhaul implementation status

Updated 2026-10-01 for BotForge 0.2.0 plus the Unreleased changes in
`CHANGELOG.md`. This is the durable checklist for the requested platform
overhaul. A checked item is implemented in the current working tree; partial
items state exactly what remains.

## Completed since 0.2.0 (Unreleased)

- [x] Team workspaces with owner/admin/developer/viewer roles, a personal
  workspace per account, moving bots between workspaces, and a sidebar
  switcher. Enforcement: application level, on every bot, site, operation,
  activity, SFTP and automation request (`docs/workspaces.md`).
- [x] Administrator oversight of every workspace and account: members, bots
  with owners and state, sites, and the latest 40 operations (deployments,
  builds, backups, restores, GitHub pushes).
- [x] GitHub publishing: create a repository from a bot's files and link it,
  or push the current files to the linked branch/folder through the Git Data
  API with the acting user's own token; `.gitignore`-aware, `.env` excluded,
  changed files only, never forced, recorded as `publish` operations.
- [x] Static site hosting on a separate listener with ZIP and GitHub
  publishing, five immutable releases and rollback, SPA/clean-URL options and
  custom 404 pages (`docs/sites.md`).
- [x] Integrated Bot Sites: one public site per Discord bot, an in-bot Page
  Studio, generated public pages with custom HTML/CSS, explicit safe-widget
  publication, and editable private site drafts that publish as immutable
  releases. Enforcement: author code runs only on the separate Sites origin;
  public page queries omit private analytics and operator/account data.
- [x] Custom domains for sites with DNS TXT ownership verification and
  six-hourly re-checks. Enforcement: the sites listener serves a custom domain
  only after verification (application level). TLS is **not** terminated by
  BotForge: certificates are issued by the reverse proxy; BotForge only answers
  its on-demand permission check.
- [x] Settings redesign (side-by-side sections, sticky navigation, folded
  password form), themed checkboxes/radios, accent-aware dark glows, and a
  corner-style preference including a fully square mode.
- [~] Target-scoped AI operator with encrypted OpenAI-compatible providers,
  private 90-day conversations, streamed tool records, Approval/Auto envelopes,
  Risa/SearxNG research, secret redaction, revision-checked journaled changes
  and undo, and direct-argv offline diagnostics. Enforcement: RBAC, approvals,
  limits, protected paths and SSRF checks are application-level; resource,
  mount, capability and network isolation are Docker-runtime-level. The
  separate privileged `botrunner` daemon is not implemented (`docs/ai-operator.md`).
- [x] AI operator follow-ups: every run limit (rounds, wall time, diagnostics,
  apply attempts, lifecycle actions, changed files/bytes, retained output) is
  enforced per run in both modes; model-initiated file applies, diagnostics,
  restarts and secure environment input are audited in Auto mode too;
  research checks addresses at connect time (DNS rebinding and redirects,
  `100.64.0.0/10` blocked) while allowing a private self-hosted search origin;
  `GET /api/v1/ai/conversations/:id/runs` restores runs, approvals,
  secure-input cards, change sets and Undo after a reload; tool-call rows use
  panel UUIDs; finished run streams are dropped after five minutes; providers
  are fully editable in Administration and the first key can be set in `/setup`.
  Startup, deploy and site-publish tools were **not** implemented; the
  operator has no such tools and the documentation no longer claims them.

Migrations added since 0.2.0 (the schema-version assertion in
`internal/store/sqlite/db_test.go` is 29):

| Migration | Purpose |
| --- | --- |
| `0024_workspaces.sql` | Workspaces and members; personal workspace backfill; `bots.workspace_id` |
| `0025_publish_operations.sql` | Rebuilds `operations` to allow the `publish` kind (rows preserved) |
| `0026_static_sites.sql` | Sites, releases and custom domains |
| `0027_bot_sites.sql` | One-to-one bot/site links, generated-page presentation, custom HTML/CSS and public-widget opt-in |
| `0028_ai_operator.sql` | Encrypted provider profiles, target chats, runs, tools, change/undo snapshots and site-aware audit identity |
| `0029_ai_tool_call_ids.sql` | `ai_tool_calls.provider_call_id`; rows are keyed by panel UUIDs (existing rows keep their id) |

Known gaps in this work:

- [ ] The AI operator has no tools for startup-command changes, deployments or
  site publication; those stay manual.

- [ ] Site release files are not included in `botpanel backup`/`restore`.
- [ ] Sites have no automation-API upload route, redirects/headers files,
  password protection, per-site analytics, bandwidth limits, or server-side
  builds; uploads are not malware-scanned.
- [ ] Workspace-level quotas (limits still apply per bot owner), ownership
  transfer of a workspace, and email invitations for people without an account.
- [ ] GitHub publishing does not support Git LFS, submodules, or pushing to
  a repository that has no commits yet.

## Completed in 0.2.0

- [x] In-house, searchable `/docs` page and internal Documentation navigation.
- [x] Widget groups/tabs, 1–3 column spans, minimum height, TTL, stale state,
  unpublish, authenticated deletion, explicit validation, and a 240-widget read
  cap with 48 changes per push.
- [x] Safe renderers for line, area, donut, gauge, heatmap, sparkline, key/value,
  Markdown, image, log, and code widgets.
- [x] Discord bot avatar reporting from ready discord.js/discord.py clients and
  avatar display in Fleet and Favorites, with runtime initials as fallback.
- [x] Authenticated Prometheus `/metrics`, disabled by default and free of
  bot/user/path labels.
- [x] Per-bot TCP/HTTP health probes against published loopback ports, including
  thresholds, startup/readiness grace, persisted state, and a guarded restart
  on an unhealthy transition.

Migrations added in this release:

| Migration | Purpose |
| --- | --- |
| `0021_widget_dashboards.sql` | Widget grouping, layout, expiry, and refresh metadata |
| `0022_bot_discord_identity.sql` | Validated Discord identity and avatar metadata |
| `0023_health_probes.sql` | Health-probe configuration and state |

## Partially completed

- [~] Widget charts have more safe renderer types, but still need true
  multi-series data, axes, legends, time axes, and threshold lines.
- [~] Tables accept richer safe cell values, but still need full sorting,
  alignment, formatting, and pagination.
- [~] Dashboard layout and lifecycle are server-backed, while per-user
  rename/hide/order and per-widget permissions remain browser-local or absent.
- [~] JavaScript and Python SDKs have the richest layout/lifecycle and Discord
  identity support; the remaining language SDKs need full option parity.
- [~] Telemetry already has a WebSocket transport, but browser dashboards still
  poll analytics rather than receiving widget updates in real time.
- [~] Container hardening already includes a non-root user, read-only runtime,
  capability drops, `no-new-privileges`, resource caps, and network disablement.
  Dedicated user namespaces, seccomp/AppArmor policy, and optional gVisor/Kata
  isolation are not yet implemented.

## Remaining: security and isolation

- [ ] Build a small privileged `botrunner` daemon with a narrow authenticated
  Unix-socket job protocol, then remove Docker-socket access from the panel.
- [ ] Enforce per-bot egress allowlists at DNS and firewall/proxy level.
- [ ] Enforce workspace disk quotas and per-bot IO throttling.
- [ ] Enforce bandwidth limits with host traffic shaping such as `tc`/HTB.
- [ ] Add dedicated seccomp/AppArmor profiles and user-namespace remapping.
- [ ] Offer gVisor or Kata as an optional isolation tier for untrusted tenants.
- [ ] Scan uploads for malware and enforce archive expansion/ratio limits.
- [ ] Verify runtime images with cosign and retain/validate their SBOMs.

The egress, disk, IO, and bandwidth settings must not be marked complete until
they are enforced outside configuration storage by the relevant kernel,
network, filesystem, or container-runtime mechanism.

## Remaining: operations and reliability

- [ ] Multi-node placement and scheduling, node drain, and migration.
- [ ] PostgreSQL backend plus point-in-time recovery, or an equivalent tested
  SQLite WAL archival and restore design.
- [ ] External bot/panel log shipping to Loki and/or syslog.
- [ ] Maintenance windows, node draining workflow, and signed self-update or
  update notification.
- [ ] Encrypted offsite S3/rclone backups, retention tiers, and scheduled
  verified restore drills.

## Remaining: developer and platform experience

- [ ] Transactional export/import of bot definitions, environment/startup
  versioning and diffs, then CLI and Terraform clients over the automation API.
- [ ] Dependency and license intelligence: npm/pip/Cargo audits, lockfile
  handling, update pull requests, and license checks.
- [~] Custom domains and automatic TLS exist for static sites (TLS through the
  reverse proxy's on-demand issuance). Multi-container bots, named volumes,
  and custom domains for bot HTTP ports remain.
- [ ] Pull-request previews and health-gated blue/green or canary rollout.
- [ ] A supported panel plugin model for custom pages and jobs.
- [ ] .NET, Deno/Bun, PHP, and opt-in custom Dockerfile runtimes.
- [ ] Signed callback action widgets, dashboard filters, and drill-down.
- [ ] SMTP-backed self-service signup/reset/alert workflows.

## Remaining: governance and compliance

- [ ] Configurable audit retention and export.
- [ ] GDPR data export and deletion workflows.
- [ ] Key-rotation user experience plus optional KMS/HSM integration.
- [ ] License and terms acceptance/versioning.
- [ ] Internationalization of hardcoded English strings.
- [ ] A complete accessibility audit and remediation pass.

## Recommended stage order

1. Botrunner privilege boundary.
2. Egress, disk, IO, and bandwidth enforcement.
3. Log shipping and offsite verified backups.
4. SMTP and account recovery workflows.
5. Config-as-code, CLI, and Terraform foundations.
6. Multi-node scheduling and PostgreSQL/PITR.
7. Governance, internationalization, and accessibility.
