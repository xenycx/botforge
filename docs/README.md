# BotForge documentation

BotForge is a single-host Discord bot panel packaged as one Go binary with an
embedded web interface. The current project version is **0.4.0**. Start with
the guides below.

## Install and operate

- [Deployment](deployment.md) — binary, systemd, reverse proxy, and first admin
- [Container deployment](container.md) — GHCR image and Docker Compose
- [OAuth setup](oauth.md) — GitHub and Discord sign-in
- [Backups](backup.md) — installation backup, verification, and restore
- [Diagnostics and footprint](footprint.md) — memory expectations and measurement

## Use and extend

- [Features](features.md) — supported behavior and limits
- [Workspaces](workspaces.md) — teams, roles, and administrator oversight
- [Bot Sites and static hosting](sites.md) — Page Studio, editable drafts, custom domains, DNS, and TLS through the proxy
- [AI operator](ai-operator.md) — providers, modes, tools, approvals, research, secret boundaries, diagnostics, and recovery
- [Custom dashboard widgets](widgets.md) — one JSON API for all supported languages
- [Automation API](automation.md) — scoped tokens and examples
- [Architecture](architecture.md) — state model and runner design
- [Isolation](isolation.md) — container boundaries and explicit non-goals
- [Implementation status](implementation-status.md) — completed and remaining platform-overhaul work
- [Changelog](../CHANGELOG.md) — versioned release notes

The source, releases, issue tracker, and container package live at
[github.com/xenycx/botforge](https://github.com/xenycx/botforge).
