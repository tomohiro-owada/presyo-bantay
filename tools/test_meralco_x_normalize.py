#!/usr/bin/env python3
"""Unittests for the Meralco mode of tools/maynilad_x_normalize.py."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
TOOL = REPO / "tools" / "maynilad_x_normalize.py"
FIX = REPO / "go" / "internal" / "utilities" / "testdata" / "meralco_x"
MERALCO = ["--account", "meralco", "--account-id", "15371662", "--originals-only"]


def run(*args):
    return subprocess.run(["python3", str(TOOL), *args], capture_output=True, text=True)


class TestMeralco(unittest.TestCase):
    def test_raw_fixture(self):
        p = run(str(FIX / "x-tool-response-2026-10-30.json"), *MERALCO,
                "--fetched-at", "2026-10-30T19:50:00+08:00")
        self.assertEqual(p.returncode, 0, p.stderr)
        d = json.loads(p.stdout)
        self.assertEqual((d["account"], d["account_id"]), ("meralco", "15371662"))
        self.assertEqual(len(d["posts"]), 25)
        for post in d["posts"]:
            self.assertTrue(post["url"].startswith("https://x.com/meralco/status/"))
        em = [x for x in d["posts"] if "emergency line trouble" in x["text"]]
        self.assertEqual(len(em), 4)
        self.assertEqual(d["posts"][0]["created_at"], "2026-10-30T19:38:14+08:00")

    def test_replies_and_retweets_dropped(self):
        raw = {"data": [
            {"id": "1", "created_at": "2026-10-30T01:00:00.000Z", "text": "Your area in Las Piñas City may have been affected by an emergency line trouble"},
            {"id": "2", "created_at": "2026-10-30T01:00:00.000Z", "text": "@someone Hi, please DM us", "in_reply_to_user_id": "9"},
            {"id": "3", "created_at": "2026-10-30T01:00:00.000Z", "text": "reply by ref", "referenced_tweets": [{"type": "replied_to", "id": "7"}]},
            {"id": "4", "created_at": "2026-10-30T01:00:00.000Z", "text": "RT @x: thing", "referenced_tweets": [{"type": "retweeted", "id": "8"}]},
            {"id": "5", "created_at": "2026-10-30T01:00:00.000Z", "text": "@leading mention only"},
        ]}
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            json.dump(raw, tf)
        p = run(tf.name, *MERALCO, "--fetched-at", "2026-10-30T12:00:00+08:00")
        self.assertEqual(p.returncode, 0, p.stderr)
        ids = [x["id"] for x in json.loads(p.stdout)["posts"]]
        self.assertEqual(ids, ["1"])
        self.assertNotIn("someone", p.stdout)

    def test_merge_and_retention(self):
        inbox = FIX / "inbox-2026-10-30.json"
        p = run(str(inbox), *MERALCO, "--merge-from", str(inbox),
                "--fetched-at", "2026-11-30T12:00:00+08:00")
        # Everything is older than 21 days at that fetched_at -> empty but
        # merge-from was given, so the output is an empty inbox, not an error.
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(json.loads(p.stdout)["posts"], [])

    def test_defaults_stay_maynilad(self):
        p = run(str(FIX / "x-tool-response-2026-10-30.json"), "--fetched-at", "2026-10-30T19:50:00+08:00")
        d = json.loads(p.stdout)
        self.assertEqual(d["account"], "maynilad")
        self.assertEqual(d["account_id"], "2992779308")

    def test_fixture_inbox_is_v1_meralco(self):
        d = json.loads((FIX / "inbox-2026-10-30.json").read_text(encoding="utf8"))
        self.assertEqual(d["version"], 1)
        self.assertEqual(d["account"], "meralco")


if __name__ == "__main__":
    unittest.main()
