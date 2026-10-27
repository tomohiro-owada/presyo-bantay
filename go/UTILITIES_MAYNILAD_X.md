# Maynilad water interruptions from X (@maynilad)

The Maynilad website is behind a Cloudflare challenge (HTTP 403), so the
Go side no longer fetches it by default. Instead a scheduled routine
(outside the VPS) reads @maynilad (user id 2992779308) with X's public
read tools and pushes a normalized v1 JSON file to
`$PRESYO_HOME/inbox/maynilad_x.json`. The Go side (`go/internal/utilities`,
`xposts.go`) reads it for the `water` section.

## Inbox format (v1)

```json
{
  "version": 1, "account": "maynilad", "account_id": "2992779308",
  "fetched_at": "2026-10-28T12:00:00+08:00",
  "posts": [ {"id": "<string>", "created_at": "2026-10-27T15:47:14+08:00",
    "text": "<full text, note_tweet preferred>",
    "url": "https://x.com/maynilad/status/<id>",
    "lang": "en", "images": ["https://pbs.twimg.com/media/....jpg"], "has_video": false} ]
}
```

`text` is always the full text (`note_tweet.text` when present; the raw
`text` field is truncated with a t.co link). `url` is canonical.
`images` come from `includes.media` (photo `url`, video
`preview_image_url` with `has_video: true`).

## Tooling (repo root `tools/`)

- `tools/maynilad_x_normalize.py` (stdlib only): raw X response or v1
  inbox in, v1 inbox out (`--merge-from FILE`, `--fetched-at RFC3339`).
- `tools/push_maynilad_x.sh INPUT.json [--dry-run]`: merge with the
  remote inbox over ssh (`PRESYO_VPS_HOST` required, never hard-coded;
  `PRESYO_INBOX_PATH` defaults to `inbox/maynilad_x.json`), validate,
  atomic upload. See `--dry-run` for offline checks.
- Tests: `tools/test_maynilad_x_normalize.py`,
  `tools/test_push_maynilad_x.sh` (fake `ssh` on PATH, no network).

## Go behaviour

- `utilities fetch -maynilad-x FILE [-maynilad-site]`: the website fetch
  is optional and disabled by default (`MayniladSite: false`). With only
  `-maynilad-x` set the `water` section works offline (local file read).
- Missing inbox file: warning only, no error. Stale inbox (`fetched_at`
  older than 12h at fetch time): warning only. Malformed JSON/version:
  error. A 403 from the site is simply not requested when
  `MayniladSite` is false, and no longer fails the section when the X
  inbox produced data.
- `WaterStatus.Advisories` carries `XAdvisory` rows (no barangay list;
  alerts use per-language `XAreasTL`/`XAreasEN`/`XAreasJA` labels). The `water` section stamps fresh whenever
  the inbox loads OK (even with zero advisories, so old alerts
  disappear). The FCDA fetch still uses the website (still blocked).
