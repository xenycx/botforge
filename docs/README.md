# BotForge documentation

BotForge is a single-host Discord bot panel packaged as one Go binary with an
embedded web interface. Start with the guides below.

## Install and operate

- [Deployment](deployment.md) — binary, systemd, reverse proxy, and first admin
- [Container deployment](container.md) — GHCR image and Docker Compose
- [OAuth setup](oauth.md) — GitHub and Discord sign-in
- [Backups](backup.md) — installation backup, verification, and restore
- [Diagnostics and footprint](footprint.md) — memory expectations and measurement

## Use and extend

- [Features](features.md) — supported behavior and limits
- [Custom dashboard widgets](widgets.md) — one JSON API for all supported languages
- [Automation API](automation.md) — scoped tokens and examples
- [Architecture](architecture.md) — state model and runner design
- [Isolation](isolation.md) — container boundaries and explicit non-goals

The source, releases, issue tracker, and container package live at
[github.com/xenycx/botforge](https://github.com/xenycx/botforge).
