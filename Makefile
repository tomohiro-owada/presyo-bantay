# Presyo Bantay top-level tasks.
#
#   make site-data   export JSON for the static site via the Go binary
#                    (data/presyo.db -> site/public/data)
#   make site-data-py  same via the legacy Python exporter
#   make site        npm ci + vite build (output: site/dist; uses committed site/public/data)
#   make test        Python test suite + site vitest suite

VENV_PY ?= .venv/bin/python

.PHONY: site-data site-data-py site test

site-data:
	cd go && go run ./cmd/presyo export -db ../data/presyo.db -out ../site/public/data

site-data-py:
	$(VENV_PY) -m presyo_bantay export-site --db data/presyo.db --out site/public/data

site:
	cd site && npm ci && npm run build

test:
	$(VENV_PY) -m pytest -q
	cd site && npm test
