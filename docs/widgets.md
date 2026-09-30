# Custom dashboard widgets

Bots can publish a dashboard made from safe, declarative widgets. The schema is
plain JSON, so it behaves the same in Node.js, Python, Go, Rust, Java, and Ruby.
The panel renders known elements; bot code cannot inject HTML, scripts, CSS, or
iframes into an operator's browser.

Send up to 48 widget changes in one telemetry request. A widget with the same
`key` replaces its previous value. The dashboard returns up to 240 active
widgets, organized into tabs by `group` and ordered by `position`.

```json
{
  "ready": true,
  "widgets": [
    {"key":"shards","kind":"metric","title":"Shards","group":"Overview","position":10,"data":{"value":4,"unit":"online"}},
    {"key":"latency","kind":"line","title":"Gateway latency","group":"Operations","span":2,"position":20,"ttl_seconds":120,"data":{"unit":"ms","points":[{"label":"now","value":42}]}},
    {"key":"gateway","kind":"status","title":"Gateway","group":"Operations","position":30,"data":{"state":"good","text":"Connected"}}
  ]
}
```

## Widget kinds

| Kind | Data fields |
| --- | --- |
| `metric` | `value` number, optional `unit` and `detail` |
| `status` | `state`: `good`, `warn`, `bad`, or `neutral`; `text` |
| `progress` | numeric `value` and `max` |
| `text` | `text` (plain text; newlines allowed) |
| `chart` | `points`: up to 20 `{label, value}` objects |
| `table` | `columns` and `rows` string arrays |
| `link` | `label` and an `http` or `https` `url` |
| `line`, `area`, `sparkline` | `points`: up to 100 `{label?, value}` objects; optional `unit` |
| `donut` | `values`: up to 30 `{label?, value}` objects |
| `gauge` | numeric `value`, optional `max`, `label`, and `unit` |
| `heatmap` | `cells`: up to 120 `{label?, value}` objects |
| `kv` | `items`: up to 30 `{key, value}` objects |
| `markdown` | `text` (displayed safely; raw HTML is never rendered) |
| `image` | absolute `http(s)` `url` and optional `alt` |
| `log` | `text` rendered as bounded monospace output |
| `code` | `code` and optional `language` |

Optional layout fields are `group` (1–48 characters), `span` (`1`, `2`, or
`3`), `min_height` (0–800 pixels), and `position` (0–1000). Set
`ttl_seconds` from 30 seconds to seven days to auto-expire a widget that is not
refreshed. Widgets that have not refreshed for five minutes show a stale badge.

Unpublish keys in the next push:

```json
{"unpublish":["queue","old-chart"]}
```

A full bot administrator can also call
`DELETE /api/v1/bots/{bot_id}/widgets/{widget_key}`.

Each shipped SDK exposes a `widget`/`SetWidget` method. Widgets share the normal
60-push/minute telemetry limit. A complete push body is capped at 64 KiB and
each widget's data at 4 KiB. Prefer updating widgets every 10–30 seconds.

This model was chosen over embedded webviews or arbitrary HTML because it is
portable across languages, survives panel theme changes, works on mobile, and
keeps untrusted bot output out of the panel's execution context.
