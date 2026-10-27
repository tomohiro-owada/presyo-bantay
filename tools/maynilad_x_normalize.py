#!/usr/bin/env python3
"""Normalize a Maynilad X tool response (or an existing v1 inbox) to v1 inbox JSON.

Reads a file that is EITHER a raw X tool response (with data[]/includes.media[])
OR an already-normalized v1 inbox. Optional --merge-from unions with an
existing inbox by id (new wins). Outputs the v1 inbox to stdout.

v1 inbox: {"version":1,"account":"maynilad","account_id":"2992779308",
 "fetched_at":"<RFC3339 with offset>",
 "posts":[{"id","created_at","text","url","lang","images","has_video"}]}

Rules: prefer note_tweet.text, canonical url
https://x.com/maynilad/status/<id>, images from includes.media (photo url,
video preview_image_url with has_video true), union with merge file by id
(new wins), newest first, drop posts older than 21 days, cap 300,
fetched_at = now (Asia/Manila offset) unless --fetched-at given.
"""

import argparse
import json
import sys
from datetime import datetime, timedelta, timezone

MANILA = timezone(timedelta(hours=8))
ACCOUNT = "maynilad"
ACCOUNT_ID = "2992779308"


def parse_time(s):
    if not isinstance(s, str) or not s:
        raise ValueError("bad time %r" % (s,))
    t = s.strip()
    if t.endswith("Z"):
        t = t[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(t)
    except ValueError:
        raise ValueError("bad time %r" % (s,))
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=MANILA)
    return dt


def normalize_raw(obj):
    if not isinstance(obj, dict) or not isinstance(obj.get("data"), list):
        raise ValueError("not a raw X tool response (missing data[])")
    media_by_key = {}
    inc = obj.get("includes") or {}
    if isinstance(inc, dict):
        for m in inc.get("media") or []:
            if isinstance(m, dict) and m.get("media_key"):
                media_by_key[m["media_key"]] = m
    posts = []
    for p in obj["data"]:
        if not isinstance(p, dict):
            continue
        pid = str(p.get("id") or "")
        if not pid:
            continue
        created = p.get("created_at") or ""
        try:
            dt = parse_time(created)
        except ValueError:
            continue
        note = p.get("note_tweet") or {}
        text = note.get("text") if isinstance(note, dict) and note.get("text") else p.get("text") or ""
        images = []
        has_video = False
        atts = p.get("attachments") or {}
        keys = atts.get("media_keys") or [] if isinstance(atts, dict) else []
        for k in keys:
            m = media_by_key.get(k)
            if not isinstance(m, dict):
                continue
            typ = m.get("type")
            if typ == "photo" and m.get("url"):
                images.append(m["url"])
            elif typ == "video" and m.get("preview_image_url"):
                images.append(m["preview_image_url"])
                has_video = True
            elif typ == "animated_gif" and m.get("preview_image_url"):
                images.append(m["preview_image_url"])
        posts.append({
            "id": pid,
            "created_at": dt.astimezone(MANILA).isoformat(),
            "text": text,
            "url": "https://x.com/maynilad/status/%s" % pid,
            "lang": p.get("lang") or "",
            "images": images,
            "has_video": has_video,
        })
    return posts


def normalize_inbox(obj):
    if not isinstance(obj, dict) or obj.get("version") != 1 or not isinstance(obj.get("posts"), list):
        raise ValueError("not a v1 inbox (want version==1 with posts[])")
    posts = []
    for p in obj["posts"]:
        if not isinstance(p, dict):
            continue
        pid = str(p.get("id") or "")
        if not pid:
            continue
        try:
            dt = parse_time(p.get("created_at") or "")
        except ValueError:
            continue
        posts.append({
            "id": pid,
            "created_at": dt.astimezone(MANILA).isoformat(),
            "text": p.get("text") or "",
            "url": p.get("url") or ("https://x.com/maynilad/status/%s" % pid),
            "lang": p.get("lang") or "",
            "images": list(p.get("images") or []),
            "has_video": bool(p.get("has_video")),
        })
    # Canonicalize urls.
    for post in posts:
        post["url"] = "https://x.com/maynilad/status/%s" % post["id"]
    return posts


def load_merge(path):
    try:
        with open(path, "r", encoding="utf-8") as fh:
            content = fh.read()
    except FileNotFoundError:
        return []
    if not content.strip():
        return []
    try:
        obj = json.loads(content)
    except json.JSONDecodeError:
        return []
    try:
        return normalize_inbox(obj)
    except ValueError:
        return []


def main(argv=None):
    ap = argparse.ArgumentParser(description="Normalize Maynilad X posts to v1 inbox JSON.")
    ap.add_argument("input", help="raw X tool response JSON or v1 inbox JSON")
    ap.add_argument("--merge-from", default=None, help="existing inbox to merge (missing/empty/invalid = no merge)")
    ap.add_argument("--fetched-at", default=None, help="override fetched_at (RFC3339)")
    args = ap.parse_args(argv)

    try:
        with open(args.input, "r", encoding="utf-8") as fh:
            raw = fh.read()
    except FileNotFoundError:
        print("error: input file not found: %s" % args.input, file=sys.stderr)
        return 1
    if not raw.strip():
        print("error: input file is empty: %s" % args.input, file=sys.stderr)
        return 1
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError as e:
        print("error: invalid JSON in %s: %s" % (args.input, e), file=sys.stderr)
        return 1

    try:
        if isinstance(obj, dict) and "posts" in obj:
            posts = normalize_inbox(obj)
        elif isinstance(obj, dict) and "data" in obj:
            posts = normalize_raw(obj)
        else:
            print("error: invalid input: want raw X response (data[]) or v1 inbox (posts[])", file=sys.stderr)
            return 1
    except ValueError as e:
        print("error: invalid input: %s" % e, file=sys.stderr)
        return 1

    if args.fetched_at:
        try:
            fetched_dt = parse_time(args.fetched_at)
        except ValueError:
            print("error: bad --fetched-at %r" % args.fetched_at, file=sys.stderr)
            return 1
        fetched_dt = fetched_dt.astimezone(MANILA)
    else:
        fetched_dt = datetime.now(MANILA)
    fetched_at = fetched_dt.isoformat()

    by_id = {}
    if args.merge_from:
        for p in load_merge(args.merge_from):
            by_id[p["id"]] = p
    for p in posts:
        by_id[p["id"]] = p

    cutoff = fetched_dt - timedelta(days=21)
    merged = []
    for p in by_id.values():
        try:
            dt = parse_time(p["created_at"])
        except ValueError:
            continue
        if dt < cutoff:
            continue
        merged.append((dt, p))
    merged.sort(key=lambda t: t[0], reverse=True)
    merged = [p for _, p in merged[:300]]

    if not merged and not args.merge_from:
        print("error: no posts after normalization (and no --merge-from)", file=sys.stderr)
        return 1

    out = {
        "version": 1,
        "account": ACCOUNT,
        "account_id": ACCOUNT_ID,
        "fetched_at": fetched_at,
        "posts": merged,
    }
    json.dump(out, sys.stdout, ensure_ascii=False, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
