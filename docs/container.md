# Container deployment

Published images are available from `ghcr.io/xenycx/botforge`. Tags named
`latest`, `X.Y.Z`, `X.Y`, and the commit SHA are produced by GitHub Actions.

```sh
curl -O https://raw.githubusercontent.com/xenycx/botforge/main/compose.yaml
curl -O https://raw.githubusercontent.com/xenycx/botforge/main/deploy/systemd/botpanel.env.example
mv botpanel.env.example .env
docker compose run --rm botforge keygen
docker compose run --rm botforge create-admin you@example.com
docker compose up -d
```

The Compose file stores the database, keys, backups, and bot workspaces in the
`botforge-data` volume. Back up that volume. The Docker socket mount is required
by the in-process runner and gives the panel root-equivalent control of the
host. Do not describe the container as a security boundary for the panel.

Use a reverse proxy for HTTPS and set `BOTPANEL_PUBLIC_URL` to its public origin.
Pin a version tag instead of `latest` for controlled upgrades.
