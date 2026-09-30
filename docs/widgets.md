# Custom dashboard widgets

Bots can publish a dashboard made from safe, declarative widgets. The schema is
plain JSON, so it behaves the same in Node.js, Python, Go, Rust, Java, and Ruby.
The panel renders known elements; bot code cannot inject HTML, scripts, CSS, or
iframes into an operator's browser.

Send up to 24 widgets in the existing telemetry request. A widget with the same
`key` replaces its previous value and is visible to every user who can view the
bot's analytics.

```json
{
  "ready": true,
  "widgets": [
    {"key":"shards","kind":"metric","title":"Shards","position":10,"data":{"value":4,"unit":"online"}},
    {"key":"queue","kind":"progress","title":"Music queue","position":20,"data":{"value":7,"max":25}},
    {"key":"gateway","kind":"status","title":"Gateway","position":30,"data":{"state":"good","text":"Connected"}}
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

Each shipped SDK exposes a `widget`/`SetWidget` method. Widgets share the normal
60-push/minute telemetry limit. A complete push body is capped at 64 KiB and
each widget's data at 4 KiB. Prefer updating widgets every 10–30 seconds.

This model was chosen over embedded webviews or arbitrary HTML because it is
portable across languages, survives panel theme changes, works on mobile, and
keeps untrusted bot output out of the panel's execution context.
