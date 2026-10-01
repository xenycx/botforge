# AI operator

BotForge’s AI operator is a private incident workspace attached to one bot or
one hosted site. It uses administrator-configured OpenAI-compatible Chat
Completions providers (DeepSeek is the first preset) and never disables normal
hosting when no provider is configured.

## Modes and approvals

**Approval** is the default. Status, permitted logs/files, environment-variable
names and web research may be inspected automatically. Diagnostic commands,
live file application, secure environment input, startup/lifecycle actions,
deployments and site publication stop on a review card.

**Auto repair** begins only after the user approves its visible action envelope
and server-owned limits. The envelope defaults to 12 model/tool rounds, five
diagnostic jobs, three apply attempts, two lifecycle/deploy/publish actions,
20 minutes, 32 files/8 MiB of text, and 2 MiB retained tool output. GitHub
writes and kill never inherit that approval. One mutating run is allowed per
target, two active runs per user, and four globally.

## Provider and search setup

Administration → Panel settings stores multiple providers. Profiles include
the base URL, chat/models paths, model, context/output limits, temperature,
timeouts and optional token prices. **Test capabilities** sends no project data
and verifies authentication, SSE streaming, usage and a harmless tool call.
`/models` discovery is optional; administrators may enter a model ID manually.
Bearer keys are AES-256-GCM sealed and every API response exposes only
`key_set`.

Research uses a configurable SearxNG/Risa endpoint (`GET /search?format=json`)
and rotating `X-API-Key` credentials. Public page fetches strip scripts,
navigation and forms. Every URL and redirect is re-resolved; loopback, private,
link-local, multicast, cloud-metadata, credential-bearing and non-HTTP URLs are
blocked. Search keys are sent only to the configured search origin.

## Data and permission boundary

Conversations belong to their creator; administrators may inspect them. They
expire after 90 inactive days or can be deleted manually. Deletion removes
messages, tool output and undo snapshots while audit events remain.

The model may receive bounded, explicitly requested safe file snippets, status,
redacted logs/tool output, environment **names**, and public research. It never
receives environment values, provider/search keys, authentication headers,
protected credential files, hidden reasoning, panel metadata, or unrestricted
workspace archives. Secure environment values travel directly from the form to
the environment service and bypass the provider and transcript.

Every tool re-checks the account and target permission immediately before it
runs. File tools require file/developer authority; environment names and secure
input require environment authority; startup requires full bot administration;
power requires power; publication requires site developer. User/access/key/MFA,
domain, resource/network-policy, backup/restore, deletion and secret-reveal
tools do not exist.

## Changes, diagnostics and recovery

AI text changes are limited to 1 MiB per file and stored as compressed
before/after snapshots with a bounded unified diff. Application checks the
original revision, stages the complete set, and uses the existing crash journal
for recoverable renames. A concurrent editor, SFTP write or deployment causes a
conflict instead of being overwritten. Undo likewise requires the retained
after-revision to still match.

Diagnostics use the runtime’s administrator-controlled YAML argv prefixes.
The container mounts only a private safe snapshot, receives no bot environment,
ports, Docker socket, devices or extra mounts, and runs non-root with a
read-only root, dropped capabilities, `no-new-privileges`, PID/CPU/memory/tmpfs
limits and `NetworkMode=none`. Arguments are direct argv; shell syntax,
interpreter eval and paths escaping `/workspace` are rejected. The default
timeout is ten minutes and the hard ceiling is twenty.

This isolation is enforced by the application plus the existing Docker runtime
adapter. The wider privileged `botrunner` daemon project is **not implemented**;
the panel process still talks to Docker. Web research is separate from the
offline command container. Active runs are marked interrupted after a panel
restart and can be retried from their retained chat.
