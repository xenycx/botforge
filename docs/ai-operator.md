# AI assistant (AI operator)

BotForge’s AI assistant is one private chat for the whole panel. It opens from
the **Ask AI** button at the bottom right of every page and from the sparkle
button in the header (`Ctrl+.`). It uses administrator-configured
OpenAI-compatible Chat Completions providers (DeepSeek is the first preset) and
never disables normal hosting when no provider is configured.

## One chat that follows you

The chat is not tied to a bot. Every message carries **what you were looking
at**: the bot or site, the tab or section in view, and for the file editor the
open file. A chip above the message box shows exactly what is shared and can be
dismissed for a single question; each sent message keeps its chip in the
transcript. Moving to another bot or page keeps the same conversation and
attaches the new context to the next message.

* The browser describes the page, but the server trusts only the bot or site
  **id**. It authorizes that id for the person, replaces any browser-supplied
  name with the real one, and clips everything else to short plain text. A bot
  the person cannot open cannot be named as context.
* The target of a run is fixed when it starts and is stored with the run. With
  no bot or site in view, the assistant answers general questions and can call
  `list_targets` and `focus_target` to look at one of the person’s bots in
  Approval mode. A run that already has a target cannot move to another, and
  **Auto repair requires a target**, so an approval always covers one bot or
  site.
* Conversations belong to their creator and are private, including from
  administrators. Chats created through the older per-bot and per-site
  endpoints keep working and keep their target.
* The system prompt gives the assistant its role, a debugging method (status,
  console output, build output, then files), safe-use rules for secrets and
  untrusted text, what it cannot do, and the current context.

## Tools

| Tool | Reads or changes | Needs |
| --- | --- | --- |
| `target_status` | State, last exit code, last error, startup argv | Access to the bot or site |
| `read_logs` | The last 20–500 lines of the bot’s console output (secrets redacted) | Console permission |
| `build_output` | Recent builds/deployments/backups and the tail of one operation’s output | Console permission |
| `list_files`, `read_file`, `search_files` | Permitted workspace text | Access |
| `environment_names` | Variable names only | Access |
| `list_targets`, `focus_target` | The bots and sites the person can open; scope a target-less run | Access |
| `web_search`, `web_fetch` | Public research | Administrator enabled |
| `request_environment_values` | Secure form; values bypass the model | Environment permission, user input |
| `propose_file_change` | One text file, with diff and Undo | File permission, approval |
| `run_diagnostic` | One allowlisted command in an offline container | File permission, approval |
| `restart_bot` | Restart | Power permission, approval |

`read_logs` and `build_output` are what make debugging possible: the assistant
sees the crash, not just the files.

## Modes and approvals

**Approval** is the default. Status, permitted files, environment-variable
names and web research may be inspected automatically. Diagnostic commands,
live file application, bot restarts and secure environment input stop on a
review card.

**Auto repair** begins only after the user approves its visible action envelope
(the tool names it covers and the run's limits). It covers isolated diagnostics,
file changes and bot restarts; secure environment input still waits for the
person. The operator has **no** tools to edit the startup command, deploy,
publish a site, push to GitHub or kill a bot, so neither mode can do those.
One mutating run is allowed per target, two active runs per user, and four
globally.

### Run limits

Every run, in both modes, is counted against server-owned limits. Approving a
card never raises a limit; a call over a limit is refused and the model receives
a tool error asking it to summarize and stop.

| Limit | Default | Counts |
| --- | --- | --- |
| Rounds | 12 | Model/tool rounds |
| Wall time | 20 minutes | From queueing to finish |
| Diagnostics | 5 | Diagnostic jobs started |
| Apply attempts | 3 | File changes the panel tried to apply (conflicts included) |
| Lifecycle actions | 2 | Bot restarts |
| Changed files | 32 | Distinct files changed by applied change sets |
| Changed bytes | 8 MiB | Written text (deleted text for deletions) |
| Retained output | 2 MiB | Stored tool output; a result that crosses it is truncated and later tools fail |

The limits are stored with each run and shown on the Auto envelope card.

### Audit

Live actions the model takes are recorded in the activity log with the person
who started the run (labelled "via AI operator", plus "(Auto)" for Auto runs),
whether or not a card was approved: `ai.file_apply` (path), `ai.diagnostic`
(redacted argv), `ai.restart` and `ai.env_input` (variable names only). Refused
and failed attempts are recorded as `denied`/`failed`. Chat requests,
approvals, secure input and undo are recorded as well.

### Restoring the chat

`GET /api/v1/ai/conversations/:id/runs` returns the latest runs (default 5, at
most 20 with `?limit=`) with their tool calls and change sets, each run’s target
and its start event. Runs whose bot or site the person can no longer open are
left out. The chat uses it after a reload and when a run finishes, so pending
approvals, secure-input cards, diffs and Undo stay available. Tool-call rows have their own
UUIDs; the provider's call id is kept separately and only echoed back to the
provider. Live event streams of finished runs are dropped after five minutes.

## Provider and search setup

The first provider and its API key can be entered in the optional **AI
operator** step of the `/setup` wizard. Administration → Panel settings stores
multiple providers and can add, edit, enable/disable, delete and re-key them.
Profiles include the base URL, chat/models paths, model, context/output limits,
temperature, timeouts and optional token prices (USD per million tokens). **Test capabilities** sends no project data
and verifies authentication, SSE streaming, usage and a harmless tool call.
**Discover models** queries the provider's models path with the saved key; it
is optional and administrators may enter any model ID manually.
Bearer keys are AES-256-GCM sealed and every API response exposes only
`key_set`.

Research uses a configurable SearxNG/Risa endpoint (`GET <base>/search?format=json`)
and rotating `X-API-Key` credentials; **Test search** checks the saved settings.
The search origin is administrator-trusted and may be a private address, such
as a self-hosted SearxNG; cloud-metadata addresses are still refused. The search
client never follows a redirect to another origin, so keys are sent only to the
configured origin.

Page fetches and search results are public-only. The address of every actual
connection (each redirect included) is checked by the dialer after DNS
resolution, which also defeats DNS rebinding: loopback, private, link-local,
carrier-grade NAT (`100.64.0.0/10`), multicast, reserved, NAT64/6to4 and
cloud-metadata addresses are refused, as are credential-bearing and non-HTTP
URLs. Page fetches are made directly and never through an HTTP proxy, because
a proxy would resolve the name itself; search requests to the configured
origin honour `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY`. A search key the service
refuses (401/403) is skipped in favour of the next configured key. Fetched pages are stripped of scripts, navigation and
forms.

## Data and permission boundary

Conversations belong to their creator. A chat with no target is private even
from administrators; administrators can still inspect the conversations that
older per-target endpoints created. Chats expire after 90 inactive days or can be
deleted manually. Deletion removes
messages, tool output and undo snapshots while audit events remain.

The model may receive bounded, explicitly requested safe file snippets, status,
redacted tool output, environment **names**, and public research. It never
receives environment values, provider/search keys, authentication headers,
protected credential files, hidden reasoning, panel metadata, or unrestricted
workspace archives. Secure environment values travel directly from the form to
the environment service and bypass the provider and transcript.

Every tool re-checks the account and target permission immediately before it
runs. File tools require file/developer authority; environment names and secure
input require environment authority; diagnostics require file authority;
restarts require power. Access is checked again after an approval card is
decided. Startup, deployment, site publication, GitHub, user/access/key/MFA,
domain, resource/network-policy, backup/restore, deletion and secret-reveal
tools do not exist.

## Changes, diagnostics and recovery

AI text changes are limited to 1 MiB per file and stored as compressed
before/after snapshots with a bounded unified diff. The reviewable diff has
configured secret values redacted; Undo restores the exact snapshot. Application checks the
original revision, stages the complete set, and uses the existing crash journal
for recoverable renames. A concurrent editor, SFTP write or deployment causes a
conflict instead of being overwritten. Undo likewise requires the retained
after-revision to still match.

Diagnostics use the runtime’s administrator-controlled YAML argv prefixes
(`diagnostic_commands` in `runtimes/*.yaml`). The defaults cover version checks,
syntax checks, the test/lint/build scripts and running the bot’s entry file
(`node index.js`, `python main.py`, …) so startup, import and syntax errors
surface; `python3` is treated as `python`. The tool description lists the
allowed prefixes for the focused bot’s runtime, and a refused command returns
the full list plus a pointer to `read_file`, `read_logs` and `build_output`, so
the model corrects itself instead of retrying a shell command. The container mounts only a private safe snapshot, receives no bot environment,
ports, Docker socket, devices or extra mounts, and runs non-root with a
read-only root, dropped capabilities, `no-new-privileges`, PID/CPU/memory/tmpfs
limits and `NetworkMode=none`. Arguments are direct argv; shell syntax,
interpreter eval (`node -e/-p`, `python -c`, `ruby -e`) and paths escaping
`/workspace` are rejected. The snapshot skips symbolic links, so tools that
rely on `node_modules/.bin` links need to be run through a script that calls
them by path, and it is capped at 60,000 files and 512 MiB. The default
timeout is ten minutes and the hard ceiling is twenty.

This isolation is enforced by the application plus the existing Docker runtime
adapter. The wider privileged `botrunner` daemon project is **not implemented**;
the panel process still talks to Docker. Web research is separate from the
offline command container. Active runs are marked interrupted after a panel
restart and can be retried from their retained chat.
