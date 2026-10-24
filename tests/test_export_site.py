"""Static-site export: compact JSON with canonical names and week summary."""

import json
import os

from presyo_bantay import db
from presyo_bantay import fx as fx_module
from presyo_bantay.cli import main

LAS_PINAS = "New Las Piñas City Public Market"
ALABANG = "Alabang Central Market"
LA_HUERTA = "La Huerta Market/Parañaque"
BICUTAN = "Bicutan Market"


def _seed_db(path):
    conn = db.connect(path)
    markets = {}
    for name in (LAS_PINAS, ALABANG, LA_HUERTA, BICUTAN):
        markets[name] = conn.execute(
            "INSERT INTO markets (name, city) VALUES (?, ?)",
            (name, db.derive_city(name)),
        ).lastrowid
    commodities = {}
    for name, cat, spec, unit in [
        ("Premium Rice (Local)", "rice", "Local", "per kg"),
        ("Premium Rice (Loca)", "rice", "Loca", "per kg"),  # un-migrated alias
        ("Bangus", "fish", None, "per kg"),
        ("Sugar (Washed)", "sugar", "Washed", "per kg"),
    ]:
        commodities[name] = conn.execute(
            "INSERT INTO commodities (name, category, spec, unit)"
            " VALUES (?, ?, ?, ?)",
            (name, cat, spec, unit),
        ).lastrowid
    prices = {
        # (date, market, commodity, price)
        ("2026-10-09", LAS_PINAS, "Premium Rice (Local)", 50.0),
        ("2026-10-09", ALABANG, "Premium Rice (Local)", 52.0),
        ("2026-10-16", LAS_PINAS, "Premium Rice (Local)", 55.0),
        ("2026-10-16", ALABANG, "Premium Rice (Loca)", 54.0),  # alias fills gap
        ("2026-10-16", LA_HUERTA, "Premium Rice (Local)", 53.456),
        ("2026-10-09", LAS_PINAS, "Bangus", 240.0),
        ("2026-10-16", LAS_PINAS, "Bangus", 270.0),
        ("2026-10-16", BICUTAN, "Bangus", 250.0),
        ("2026-10-16", LAS_PINAS, "Sugar (Washed)", 80.0),
    }
    for day, market, commodity, price in prices:
        conn.execute(
            "INSERT INTO prices (date, market_id, commodity_id, price)"
            " VALUES (?, ?, ?, ?)",
            (day, markets[market], commodities[commodity], price),
        )
    fx_module.upsert_rates(conn, {"2026-10-16": 62.968})
    conn.commit()
    return conn


def _load(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def test_export_site_writes_compact_canonical_json(tmp_path, capsys):
    db_path = str(tmp_path / "t.db")
    out_dir = str(tmp_path / "site")
    conn = _seed_db(db_path)
    conn.close()
    assert main(["export-site", "--db", db_path, "--out", out_dir]) == 0
    capsys.readouterr()

    raw_meta = open(os.path.join(out_dir, "meta.json"), encoding="utf-8").read()
    meta = json.loads(raw_meta)
    # Compact: no indentation.
    assert "\n" not in raw_meta.strip()
    assert meta["updated"] == "2026-10-16"
    assert meta["date_min"] == "2026-10-09"
    assert meta["date_max"] == "2026-10-16"
    assert "generated_at" in meta
    assert meta["fx_source"] == (
        "USD/PHP: European Central Bank reference rates "
        "via Frankfurter (frankfurter.dev)"
    )
    assert "da.gov.ph" in meta["price_source"]

    names = [c["name"] for c in meta["commodities"]]
    assert "Premium Rice (Local)" in names
    assert "Premium Rice (Loca)" not in names  # canonicalized at export
    assert "Bangus" in names
    sugar = [c for c in meta["commodities"] if c["name"] == "Sugar (Washed)"][0]
    assert sugar["group"] == "sugar"
    assert [c for c in meta["commodities"] if c["name"] == "Bangus"][0]["group"] is None

    shorts = {m["name"]: m["short"] for m in meta["markets"]}
    assert shorts[LAS_PINAS] == "Las Piñas"
    assert shorts[ALABANG] == "Alabang"
    assert shorts[LA_HUERTA] == "La Huerta"
    assert shorts[BICUTAN] == "Bicutan"
    # Default markets: the 4 markets of interest, Las Piñas first.
    assert len(meta["default_markets"]) == 4
    by_id = {m["id"]: m["name"] for m in meta["markets"]}
    assert by_id[meta["default_markets"][0]] == LAS_PINAS

    # One compact series file per commodity covering all markets.
    rice_id = [c["id"] for c in meta["commodities"]
               if c["name"] == "Premium Rice (Local)"][0]
    series = _load(os.path.join(out_dir, "series", f"c{rice_id}.json"))
    assert series["d"] == ["2026-10-09", "2026-10-16"]
    las_id = str([m["id"] for m in meta["markets"] if m["name"] == LAS_PINAS][0])
    alb_id = str([m["id"] for m in meta["markets"] if m["name"] == ALABANG][0])
    assert series["m"][las_id] == [50.0, 55.0]
    # Alias-row price merged under the canonical series (rounded 2dp).
    assert series["m"][alb_id] == [52.0, 54.0]
    huerta_id = str([m["id"] for m in meta["markets"] if m["name"] == LA_HUERTA][0])
    assert series["m"][huerta_id] == [None, 53.46]

    fx = _load(os.path.join(out_dir, "fx.json"))
    assert fx == {"d": ["2026-10-16"], "r": [62.968]}

    week = _load(os.path.join(out_dir, "week.json"))
    assert week["latest"] == "2026-10-16"
    assert week["baseline"] == "2026-10-09"  # 7 days earlier, in 5..10 window
    rice = [i for i in week["items"] if i["name"] == "Premium Rice (Local)"][0]
    assert rice["m"][las_id] == {"p": 55.0, "b": 50.0, "c": 10.0}
    # Cheapest among the default 4 and among all markets.
    assert rice["cheap_all"]["p"] == 53.46
    assert set(rice["cheap4"]["m"]) <= set(rice["cheap_all"]["m"]) | {alb_id, las_id}
    assert len(week["items"]) == len(meta["commodities"])


def test_export_site_empty_db(tmp_path, capsys):
    db_path = str(tmp_path / "empty.db")
    out_dir = str(tmp_path / "site")
    conn = db.connect(db_path)
    conn.close()
    assert main(["export-site", "--db", db_path, "--out", out_dir]) == 0
    capsys.readouterr()
    meta = _load(os.path.join(out_dir, "meta.json"))
    assert meta["updated"] is None
    assert meta["commodities"] == []
    assert _load(os.path.join(out_dir, "fx.json")) == {"d": [], "r": []}
    assert _load(os.path.join(out_dir, "week.json"))["items"] == []
