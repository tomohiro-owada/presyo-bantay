-- Presyo Bantay SQLite store (D1-compatible: plain SQLite, no extensions,
-- no PRAGMA-dependent features, simple INTEGER/TEXT/REAL types only).

CREATE TABLE IF NOT EXISTS markets (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  city TEXT
);

CREATE TABLE IF NOT EXISTS commodities (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  category TEXT,
  spec TEXT,
  unit TEXT
);

CREATE TABLE IF NOT EXISTS prices (
  date TEXT NOT NULL,
  market_id INTEGER NOT NULL REFERENCES markets(id),
  commodity_id INTEGER NOT NULL REFERENCES commodities(id),
  price REAL NULL,
  price_low REAL NULL,
  price_high REAL NULL,
  source_url TEXT,
  UNIQUE(date, market_id, commodity_id)
);

CREATE INDEX IF NOT EXISTS idx_prices_market_commodity_date
  ON prices (market_id, commodity_id, date);

-- Dates ingested from payloads flagged "synthetic": true (allowed only with
-- --allow-synthetic; used for the offline test suite).
CREATE TABLE IF NOT EXISTS synthetic_dates (
  date TEXT PRIMARY KEY
);
