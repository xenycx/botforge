# Changelog

All notable BotForge changes are recorded here. The project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); `VERSION` is the
authoritative current version.

## Unreleased

### Added

- Team workspaces. Every account now has a personal workspace, and anyone can
  create team workspaces with owner, admin, developer, and viewer roles that
  apply to every bot and site in them, on top of per-bot sharing. A sidebar
  switcher scopes the overview, Sites, and new bots to one workspace; bots can
  be moved between workspaces.
- Administrator oversight: Administration now lists every workspace with its
  owner, members, running bots, memory, and sites, and shows each workspace's
  and each account's bots, sites, and recent deployments and operations.
- Static site hosting on a separate listener (`BOTPANEL_SITES_LISTEN`,
  `BOTPANEL_SITES_BASE_URL`): publish a ZIP or a GitHub branch, keep five
  immutable releases with instant rollback, single-page-app and clean-URL
  options, and custom 404 pages. Sites are served only on their own host names,
  never on the panel's origin, and dot-files are never served.
- Custom domains for sites, served only after DNS TXT ownership verification,
  with record instructions (CNAME or A/AAAA), periodic re-checks, and an
  on-demand TLS permission endpoint for Caddy. Administrators can suspend and
  restore any site.
- Publishing bots to GitHub: create a repository (personal or organization,
  private by default) from a bot's files and link the bot to it, or push the
  current files to the linked branch. Pushes use the acting user's own GitHub
  access, honor `.gitignore`, always exclude `.env` files, dependencies and
  `.git`, only upload changed files, show every file before anything is sent,
  are recorded as operations, and are never forced.
- Appearance setting for corner style (Square, Subtle, Default, Rounded).
  Square removes rounding everywhere, including badges and avatars.
- Migrations 24–26: workspaces and membership (with a personal workspace for
  every existing account and every bot moved into its owner's), the `publish`
  operation kind, and static sites, releases, and domains.

### Changed

- Redesigned the Settings pages: each setting's explanation sits beside its
  controls on wide screens and above them on narrow ones, the section
  navigation stays in view while scrolling, the password form is folded until
  needed, and sessions, keys, and tokens are grouped in cards. Most settings
  pages no longer need scrolling on a desktop screen.
- The account menu is reduced to Profile, Settings and Sign out; the theme
  stays one click away in the header.
- Checkboxes and radio buttons are drawn from the theme in both states instead
  of the browser's default (which rendered a flat grey box when unchecked).
- Dark-mode glows and focus shadows now follow the chosen accent color instead
  of always being orange.
- Transferring a bot moves it into the new owner's personal workspace, so the
  previous owner does not keep access through workspace membership.
- GitHub webhook deliveries for a commit the panel itself pushed no longer
  redeploy (and restart) the bot.

### Fixed

- The session cap could evict the newest session instead of the oldest when
  several sessions were created in the same millisecond.
- The OAuth provider setup steps (administration settings and the setup
  wizard) no longer scroll sideways on phones.
- `go.mod` now lists `golang.org/x/net`, `golang.org/x/mod`,
  `github.com/pkg/sftp`, and `github.com/docker/go-connections` as direct
  requirements.

### Security

- Site files never share the panel's origin; configurations where the panel
  could be addressed as a site host are refused at startup, and in production
  the panel's host may not be the sites domain itself.
- Custom domains cannot be the panel's host or its subdomains, and an
  unverified claim never reserves a domain.
- Workspace and site access checks are enforced by the application on every
  request; non-members receive `404` so identifiers cannot be probed.
- Hosted site files are not part of `botpanel backup` yet; back up
  `BOTPANEL_SITES_DIR` separately.

## 0.2.0 - 2026-10-01

### Added

- Added an in-house, searchable `/docs` experience and changed the product's
  Documentation link to stay inside BotForge.
- Expanded bot dashboards with groups/tabs, responsive spans, minimum heights,
  TTL expiry, stale states, safe Markdown, images, logs, code, key/value data,
  gauges, heatmaps, donuts, sparklines, and line/area charts.
- Added widget lifecycle controls: telemetry unpublish, an authenticated delete
  endpoint, and a dashboard Remove action.
- Added Discord identity reporting to the JavaScript and Python SDKs so a
  connected bot's Discord avatar appears in Fleet and Favorites, with runtime
  initials retained as the fallback.
- Added an opt-in, bearer-protected Prometheus `/metrics` endpoint for panel,
  database, aggregate bot, node, and health-probe measurements.
- Added per-bot TCP and HTTP health probes with startup grace, thresholds,
  loopback-only targets, persistent health state, and guarded restart-on-unhealthy.
- Added database migrations 21–23 for widget layouts/lifecycle, Discord
  identity, and health-probe configuration.
- Added a single authoritative `VERSION` file, synchronized frontend package
  metadata, and an automated consistency check (`make version-check`).

### Changed

- Increased widget updates per telemetry push from 24 to 48 and active widgets
  returned per bot to 240.
- Replaced silent widget coercion with field- and kind-specific validation
  errors, and corrected telemetry decode errors to list widgets, unpublish, and
  identity fields.
- Updated all SDK caps and added full widget layout/lifecycle helpers to the
  JavaScript and Python SDKs.
- Documented the metrics endpoint, widget schemas, health probes, and their
  operational and security boundaries.

### Security

- Discord bot tokens are not decrypted or reused for avatar lookup. Updated
  SDKs report a strictly validated Discord snowflake and CDN avatar URL only
  while their Discord client is ready.
- HTTP probes ignore environment proxies, reject redirects, and can only probe
  an existing published TCP port on `127.0.0.1`.
- Metrics are disabled unless configured with a token of at least 24 characters;
  bearer comparison is constant-time and metric labels omit bot/user identity.

### Known gaps

- The panel still directly owns the root-equivalent Docker socket; the planned
  privileged botrunner boundary has not been implemented.
- Egress, workspace disk/IO, and bandwidth policies are not kernel-enforced.
- Multi-node scheduling, PostgreSQL/PITR, external log shipping, SMTP,
  config-as-code, and the governance work remain planned. See
  `docs/implementation-status.md` for the complete checklist.

## 0.1.0 - 2026-10-01

### Added

- Initial BotForge release: single-host bot lifecycle management, six runtime
  recipes, hardened Docker containers, embedded Svelte interface, SQLite state,
  encrypted secrets, live console, deployments, backups, SFTP, OAuth, sharing,
  schedules, telemetry, and the automation API.
