# Changelog

All notable BotForge changes are recorded here. The project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); `VERSION` is the
authoritative current version.

## Unreleased

### Added

- **Email through Mailgun** (`docs/email.md`), off until an administrator
  configures it in Administration → Panel settings → Email (Mailgun) or with
  `BOTPANEL_MAILGUN_API_KEY`, `BOTPANEL_MAILGUN_DOMAIN`,
  `BOTPANEL_MAILGUN_REGION` (`us` or `eu`) and `BOTPANEL_MAIL_FROM`. The key is
  stored encrypted and never shown again; values in the environment file win
  and show as read-only. **Send test** checks the key, region and the domain's
  DNS verification and sends one message, with plain-language errors.
- **Forgot your password?** on the sign-in page: an emailed one-use link that
  expires after an hour, replaces any earlier link, and signs the account out
  everywhere when used. The request answers identically for unknown and
  disabled accounts, so it cannot be used to discover accounts, and the token
  travels in the URL fragment. It appears only when email and the panel address
  are both set (migration `0033`).
- **Invitation emails**: tick "Email the link to this address" when inviting a
  user. The invitation and its link are always created and shown; if the email
  cannot be sent the dialog says why.
- **Bot alerts by email** to the bot's owner, for the events that already post
  to Discord (crash, deploy, backup, stopped reporting). Switch it off in
  Settings → Profile → Email.
- **Security notices** by email when a password is changed or reset, or
  two-step sign-in is turned on or off. These cannot be switched off.
- **Announcements** (Administration → Announcements): write HTML news or a
  policy update, preview it in a sandboxed frame, send yourself a test (required
  before sending), then email every enabled account or administrators only. A
  *notice* reaches everyone in the audience; *news* skips accounts that turned
  off "Email me news and announcements" in Settings → Profile (migration
  `0034`). Messages go out in batches of up to 100 so each person sees only
  their own address, with a plain-text alternative, a footer saying why they
  received it, and scripts, frames, forms and event handlers stripped. Limits:
  80 KB of HTML, 1,000 recipients, one at a time and one a minute (tests
  exempt). Nothing is stored: no drafts, history or queue.
- Activity record entries for test emails, announcements, news and alert-email
  changes and password resets.

### Fixed

- **Security:** setting a password with no session to keep did not sign the
  account out anywhere, because the SQL compared against NULL
  (`token_hash != NULL`). `botpanel reset-password`, which documents that it
  signs the account out everywhere, now really does; the new emailed reset
  relies on it. Sessions created before the fix are not affected by past resets
  retroactively: reset again, or use "sign out other sessions", if a past reset
  was meant to end a compromise.

### Operational notes

- Idle memory is unchanged: email uses the standard library only, with no queue,
  connection pool or background goroutine (measured in `docs/footprint.md`).
  Sending is capped at 20 emails per recipient and 300 per hour; at most two
  sends run at once and extras are dropped and logged, not buffered.
- No Mailgun webhooks: bounces and complaints are not tracked, and nothing is
  retried. A Mailgun sandbox domain only delivers to authorized recipients.
- Migrations `0033` (`password_resets`, `users.email_alerts`) and `0034`
  (`users.email_news`) default to on, which only matters once Mailgun is
  configured and an administrator sends something.

## 0.4.0 - 2026-10-01

### Added

- **Host page rebuilt** (Administration → Host) as four tabs. *Resources* shows
  live CPU, memory (with cache and swap), load average, disk, network and disk
  throughput, with history charts for 1 hour to 30 days (averages with the
  peaks kept as dashed lines, hover read-outs), a disk-fill forecast, the
  machine (OS, kernel, CPU, uptime), the BotForge process (memory, goroutines,
  open files, request counts and errors, database size), the Docker runner
  (version, cgroups, containers, builds, queue) and storage per location with
  filesystem and inode use. A "needs attention" list calls out a nearly full
  disk, memory pressure, a Docker problem, an overloaded CPU or recent errors.
  *Bots* lists every bot with its CPU, memory against its limit, network,
  processes and workspace size, sortable and refreshed every 10 seconds.
  *Capacity* keeps the admission budgets and now shows each limit's current
  value and where it comes from. *Panel logs* tails the panel's own log from
  memory with level filter, search, live follow, expandable fields, copy and
  download; secrets, tokens and passwords are redacted before a line is kept.
  Host samples now also record load, swap, network and disk throughput
  (migration `0032`).
- **Environment page** (Administration → Environment): every `BOTPANEL_`
  variable with its description, default and source (default, environment file
  or set here), editable from the browser. Values are checked, including the
  whole configuration, before they are stored; secrets (the metrics token) are
  stored encrypted and never shown. Changes apply after a restart; the page
  shows what is waiting, and a **Restart panel** button (exit code 75, for
  systemd or a container restart policy) appears when a supervisor is
  detected. Variables read before the database opens or able to lock the panel
  out stay in the environment file (migration `0031`). `botpanel env` and
  `botpanel env reset` list and drop the saved values from the host if a bad
  save ever has to be undone; a saved set that no longer validates is ignored
  at start and reported on the page instead of stopping the panel.
- **Go to (Ctrl+K)** now finds everything: every page, your settings, every
  administration section, the Host tabs, panel logs, environment variables,
  AI provider and research settings, documentation sections, bots and their
  sections (`bluntly files`), sites, users, workspaces, AI chats and actions
  (Ask AI, new chat, new bot, switch theme, sign out). Several words narrow
  the search, Tab switches between scopes, `>` lists actions only, and an
  empty search lists everything grouped.

### Changed

- The AI settings in Panel settings use the full width: providers on the left,
  web research on the right, a guided provider form (presets for DeepSeek,
  OpenAI, OpenRouter or a custom endpoint, required fields marked, tuning and
  pricing folded away under an **Optional** heading) and an **Optional** tag on
  every field that can be skipped.
- Administration → Host moved its budgets table to the Capacity tab; the
  `/api/v1/nodes/:id/telemetry` samples gain `load1`, swap, network and disk
  throughput fields.

### Security

- Environment edits, restarts and panel-log reads are administrator-only, and
  edits and restarts are recorded in the activity log by variable name, never
  by value.

## 0.3.0 - 2026-10-01

### Added

- The AI assistant is now one panel-wide chat. **Ask AI** (bottom right of every
  page, the sparkle button in the header, or `Ctrl+.`) opens a compact
  chat window that follows you from page to page. Each message carries what you
  were looking at, shown as a chip you can dismiss: the bot or site, the tab or
  section, and the open file. The server authorizes the bot or site id itself
  and uses its real name. Chats are private to their creator, listed under a
  history button, and a failed bot shows **Ask AI why**.
- New assistant tools: `read_logs` (the bot's recent console output),
  `build_output` (recent builds and deployments with the tail of their log),
  and `list_targets` / `focus_target` so a chat started away from any bot can
  look at one of your bots. The assistant now has a full system prompt with a
  debugging method, safety rules, its limits and the current context.
- Runtime diagnostic allowlists now include version checks, syntax checks and
  running the bot's entry file (`node index.js`, `python main.py`, …), so
  startup, import and syntax errors show up. A refused command now returns the
  list of allowed commands and points to the file and log tools, and the
  `run_diagnostic` tool description lists the allowed prefixes for the bot's
  runtime.
- The bot page uses a new layout: an identity card with **Open Studio** and
  **Adjust resources**, one row of tabs, a status strip with live CPU, memory,
  disk and network readings and Start/Restart/Stop/Kill, and a terminal-style
  console as the default **Manage** tab.

- AI Operator incident workspaces for bots and sites, with encrypted
  OpenAI-compatible provider profiles (DeepSeek preset), configurable
  Risa/SearxNG research, private retained chats, streamed tool activity,
  Approval and bounded Auto repair modes, secure environment input, atomic
  revision-checked file changes with undo, and isolated offline diagnostics.
- The first AI provider and its API key can be configured in a new optional
  **AI operator** step of the `/setup` wizard.
- Administration → Panel settings can now edit, enable/disable, delete and
  re-key AI providers with every field (chat/models paths, context size, max
  output tokens, temperature, timeout, prices), discover models from the
  provider, and test the saved web-search settings.
- `GET /api/v1/ai/conversations/:id/runs` returns a conversation's latest runs
  with their tool calls and change sets. The AI workspace uses it to restore
  the active run, pending approvals, secure-input cards, change sets and Undo
  after a reload and after a run finishes.
- Model-initiated AI file applies, diagnostics, restarts and secure
  environment input are recorded in the activity log (`ai.file_apply`,
  `ai.diagnostic`, `ai.restart`, `ai.env_input`), in Auto mode too.

- Bot Sites: every Discord bot can now create one integrated public page from
  its **Public page** tab. Page Studio controls the headline, introduction,
  theme, accent, custom HTML and custom CSS; developers may explicitly publish
  safe declarative bot widgets while raw analytics, command usage, events,
  logs, configuration and account identity remain private.
- Site file workspaces: edit HTML, CSS, JavaScript and assets with the same
  explorer/editor experience as bot files. Changes stay in a private draft
  until **Publish draft** creates and activates an immutable release, so the
  normal release history and rollback workflow still applies.
- Generated bot pages and full custom-file sites can share the same address,
  domains and release history and switch modes without deleting either body of
  work. Widget markup exposes stable `data-widget` and `kind-*` CSS hooks.
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

- The AI operator tab on bots and the AI section on site pages are gone; the
  chat replaced them. The per-bot and per-site conversation endpoints still
  work and keep their target, and `GET`/`POST /api/v1/ai/conversations` list and
  create the new target-less chats (migration 0030 rebuilds the conversation
  table; existing chats, runs, tool calls and undo snapshots are preserved).
  Messages accept an optional `context` object, and a run records its own
  `bot_id`/`site_id`.
- Auto repair now needs a bot or site in view and its approval card names that
  target; a run cannot move to another bot after it starts.
- Bot page tabs were reorganised: the former Console tab is **Manage**
  (`?tab=console` and `?tab=ai` still open it), Deployments is **Deploy**,
  Environment is **Env**, and Public page is **Page**.
- AI web-search requests honour `HTTP(S)_PROXY` (the search origin is
  administrator-trusted); public page fetches still connect directly.
- AI run limits (diagnostics, apply attempts, lifecycle actions, changed
  files and bytes, retained tool output) are now enforced per run in both
  Approval and Auto mode; a call over a limit returns a tool error to the
  model. The Auto envelope lists the covered tools and the actual limits.
- The AI operator documentation, `/docs` page and Auto envelope no longer
  claim startup, deployment or site-publish actions; the operator has no such
  tools.
- AI tool-call rows use panel UUIDs; the provider's call id is kept in the new
  `provider_call_id` column (migration `0029_ai_tool_call_ids.sql`).
- The configured search base URL may include a path prefix
  (`<base>/search`).
- Renamed the bot Analytics tab to **Public page** and made Page Studio its
  default surface. Private telemetry, command activity, events and SDK setup
  remain available under **Private insights** in that tab.
- Reworked the in-product documentation into a visual guide with feature
  diagrams, callouts, runnable payload examples, privacy boundaries and a
  dedicated Bot Sites authoring flow.
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
- The bot header is one compact bar (back, name, state, live usage, power
  controls) that stays in view under the top bar, so most bot sections fit a
  desktop screen without scrolling.
- Files is a workbench sized to the window: a foldable explorer with an icon
  toolbar, a full-height editor with a status bar, and a full-screen mode.
- Health & alerts shows the heartbeat, probe and notification state side by
  side, explains every probe setting, exposes the success threshold, links to
  the SDK setup and the expanded health guide, and hides probe timing until a
  probe type is chosen.
- Bot Settings, Startup and Access, and the administration Sites, Host, Panel
  settings and Diagnostics pages use the full width: explanations beside
  controls, stat tiles, sign-in providers side by side, a budgets table, and
  search and filters directly above the sites table.
- Lists, notices, metric tiles and status panels follow the corner-style
  preference instead of always being square.

### Fixed

- AI diagnostics always failed: the runner's reconciler mistook each
  diagnostic container (labelled with the bot id) for a stale duplicate of the
  bot and stopped it, so every run ended with exit code 137 and no output.
  Diagnostic containers are now left alone and orphaned ones are removed on the
  next resync.
- AI diagnostics failed with "mount path must be absolute" when the panel ran
  with a relative data directory (the default for `.dev-data`); the scratch
  directory is now resolved to an absolute path.
- The assistant could not debug a crashed bot because it had no way to see
  console or build output and its only command tool refused almost everything
  with "not allowed by the runtime policy"; see the new tools and allowlists
  under Added.
- `node --eval=…` and `--print=…` were not recognised as interpreter evaluation
  flags, and `python -c` was refused even after `-m pytest`.
- Every real AI run failed on its first provider request: tools without
  parameters were sent with `"required": null`, which DeepSeek (and other
  strict providers) reject. Found by testing against the live API.
- A provider's invalid-request reply (unknown model, rejected parameter) is
  now shown to the user instead of a generic failure.
- **Test search** reports a failing search service as a 502 with its reason
  instead of an internal error; a search key refused with 401/403 is skipped
  in favour of the next configured key.
- AI chat Markdown renders bold and italics, tables that follow a paragraph
  line, and level 4–6 headings; the transcript and run inspector scroll
  inside the workspace instead of growing the page.
- Saving AI web-research settings failed with "invalid JSON body" because the
  form sent back the read-only `key_count`/`key_set` fields, so search stayed
  disabled. The form now sends only its inputs, shows its own errors, and
  **Test search** waits until changes are saved (it tests saved settings).
- AI runs no longer fail with a storage error when a provider reuses a tool
  call id (such as `call_0`) across runs.
- An approval decided right after its card appeared could be lost, leaving
  the run waiting; decided approvals are no longer written back as pending
  when the run finishes. Deciding a call whose run is no longer waiting now
  returns a conflict.
- Finished AI run event streams are dropped from memory after five minutes.
- A data race between starting an AI run and returning it.
- Bot lists order bots created in the same millisecond deterministically
  (by name, then id), which also fixes a flaky test.
- A failed AI run now ends its live stream with an error event, so the
  workspace no longer shows the run as still working.
- AI provider error codes (for example rate-limit or authentication codes)
  are now read from provider responses instead of being lost.
- The session cap could evict the newest session instead of the oldest when
  several sessions were created in the same millisecond.
- The OAuth provider setup steps (administration settings and the setup
  wizard) no longer scroll sideways on phones.
- The workspace switcher in the collapsed sidebar opens beside the rail above
  the page instead of underneath page content, and row menus in rounded or
  scrolling lists are no longer clipped.
- `go.mod` now lists `golang.org/x/net`, `golang.org/x/mod`,
  `github.com/pkg/sftp`, and `github.com/docker/go-connections` as direct
  requirements.

### Deprecated

- `GET`/`POST /api/v1/bots/:id/ai/conversations` and
  `/api/v1/sites/:sid/ai/conversations` are superseded by
  `/api/v1/ai/conversations` with a message `context`. They keep working and
  are not scheduled for removal yet.

### Security

- Change-set diffs shown for AI file changes redact configured secret values;
  Undo still restores the exact original bytes.
- AI web research validates the address of every connection at dial time,
  covering DNS rebinding and redirects, and now also blocks carrier-grade NAT
  (`100.64.0.0/10`), reserved, NAT64 and 6to4 ranges. Research connections no
  longer go through an environment proxy. The administrator-configured search
  origin may be private (self-hosted SearxNG); page fetches stay public-only
  and search requests never follow a redirect to another origin.
- AI tools reauthorize every action, redact configured secret values, exclude
  credential paths, reject SSRF and shell execution, and run diagnostics in
  restricted Docker containers mounting only a private safe snapshot. The
  model receives environment names but never their values.

- Author HTML, CSS and scripts for generated bot pages execute only on the
  separate Sites origin. The panel preview is a cross-origin frame, and public
  widget publication is off by default and never includes private analytics.
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
