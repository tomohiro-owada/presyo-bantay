#!/usr/bin/env python3
"""Unittests for tools/maynilad_x_normalize.py on the real fixture."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
TOOL = REPO / "tools" / "maynilad_x_normalize.py"
FIXTURE = REPO / "go" / "internal" / "utilities" / "testdata" / "maynilad_x" / "x-tool-response-2026-10-28.json"


def run(*args):
    p = subprocess.run(["python3", str(TOOL), *args], capture_output=True, text=True)
    return p


class TestNormalize(unittest.TestCase):
    def test_raw_fixture_prefers_note_tweet_and_canonical_url(self):
        with tempfile.NamedTemporaryFile(suffix=".json", delete=False) as tf:
            out_path = tf.name
        p = subprocess.run(
            ["python3", str(TOOL), str(FIXTURE), "--fetched-at", "2026-10-28T12:00:00+08:00"],
            capture_output=True, text=True,
        )
        self.assertEqual(p.returncode, 0, p.stderr)
        d = json.loads(p.stdout)
        self.assertEqual(d["version"], 1)
        self.assertEqual(d["account"], "maynilad")
        self.assertEqual(d["account_id"], "2992779308")
        self.assertEqual(d["fetched_at"], "2026-10-28T12:00:00+08:00")
        by_id = {x["id"]: x for x in d["posts"]}
        # Water-audit post: full text preferred over truncated text.
        audit = by_id["2111361327537279200"]
        self.assertIn("water interruption", audit["text"])
        self.assertNotIn("https://t.co/", audit["text"].split("Maynilad Alerts")[0][-40:] if "Maynilad Alerts" in audit["text"] else audit["text"][-40:])
        self.assertIn("October 22, 2026", audit["text"])
        self.assertEqual(audit["url"], "https://x.com/maynilad/status/2111361327537279200")
        # Emergency post has no images? just check canonical url.
        em = by_id["2110911445357772872"]
        self.assertEqual(em["url"], "https://x.com/maynilad/status/2110911445357772872")
        self.assertIn("PAGCOR", em["text"])
        # Video post gets preview image + has_video.
        vid = by_id["2114987269031600161"]
        self.assertTrue(vid["has_video"])
        self.assertTrue(any("amplify_video_thumb" in u for u in vid["images"]))
        # Photo post.
        photo = by_id["2114943110035939485"]
        self.assertFalse(photo["has_video"])
        self.assertTrue(any(u.startswith("https://pbs.twimg.com/media/") for u in photo["images"]))
        # Newest first.
        times = [x["created_at"] for x in d["posts"]]
        self.assertEqual(times, sorted(times, reverse=True))

    def test_normalized_input_roundtrip(self):
        p0 = run(str(FIXTURE), "--fetched-at", "2026-10-28T12:00:00+08:00")
        self.assertEqual(p0.returncode, 0, p0.stderr)
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write(p0.stdout)
            tf.flush()
            p1 = run(tf.name, "--fetched-at", "2026-10-28T12:00:00+08:00")
        self.assertEqual(p1.returncode, 0, p1.stderr)
        self.assertEqual(json.loads(p1.stdout)["posts"], json.loads(p0.stdout)["posts"])

    def test_merge_union_new_wins_and_old_dropped(self):
        p0 = run(str(FIXTURE), "--fetched-at", "2026-10-28T12:00:00+08:00")
        d0 = json.loads(p0.stdout)
        # Merge file with an overlapping id (stale text) + a very old post.
        merge_posts = [
            dict(d0["posts"][0], text="STALE TEXT"),
            {"id": "old999", "created_at": "2026-01-01T00:00:00+08:00", "text": "ancient",
             "url": "https://x.com/maynilad/status/old999", "lang": "en", "images": [], "has_video": False},
        ]
        merge = {"version": 1, "account": "maynilad", "account_id": "2992779308",
                 "fetched_at": "2026-10-20T00:00:00+08:00", "posts": merge_posts}
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as mf:
            json.dump(merge, mf)
            mpath = mf.name
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as inf:
            json.dump({"version": 1, "account": "maynilad", "account_id": "2992779308",
                       "fetched_at": "2026-10-28T12:00:00+08:00", "posts": d0["posts"][:2]}, inf)
            ipath = inf.name
        p = run(ipath, "--merge-from", mpath, "--fetched-at", "2026-10-28T12:00:00+08:00")
        self.assertEqual(p.returncode, 0, p.stderr)
        d = json.loads(p.stdout)
        by_id = {x["id"]: x for x in d["posts"]}
        # New wins over stale merge text.
        self.assertNotEqual(by_id[d0["posts"][0]["id"]]["text"], "STALE TEXT")
        # 21-day cutoff drops the January post.
        self.assertNotIn("old999", by_id)
        # Cap respected.
        self.assertLessEqual(len(d["posts"]), 300)

    def test_invalid_input_and_empty_result_fail(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write('{"nope": true}')
            bad = tf.name
        p = run(bad)
        self.assertNotEqual(p.returncode, 0)
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write('{"data": [], "includes": {}}')
            empty = tf.name
        p = run(empty)
        self.assertNotEqual(p.returncode, 0)

    def test_missing_merge_file_is_ok(self):
        p0 = run(str(FIXTURE), "--fetched-at", "2026-10-28T12:00:00+08:00")
        d0 = json.loads(p0.stdout)
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as inf:
            json.dump({"version": 1, "account": "maynilad", "account_id": "2992779308",
                       "fetched_at": "2026-10-28T12:00:00+08:00", "posts": d0["posts"][:1]}, inf)
            ipath = inf.name
        p = run(ipath, "--merge-from", "/nonexistent-merge.json", "--fetched-at", "2026-10-28T12:00:00+08:00")
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(len(json.loads(p.stdout)["posts"]), 1)


if __name__ == "__main__":
    unittest.main()
