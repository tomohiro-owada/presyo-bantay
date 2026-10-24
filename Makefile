# Presyo Bantay top-level tasks.
#
#   make site-data   export JSON for the static site (data/presyo.db -> site/public/data)
#   make site        site-data + npm ci + vite build (output: site/dist)
#   make test        Python test suite + site vitest suite

VENV_PY ?= .venv/bin/python

.PHONY: site-data site test

site-data:
	$(VENV_PY) -m presyo_bantay export-site --db data/presyo.db --out site/public/data

site: site-data
	cd site && npm ci && npm run build

test:
	$(VENV_PY) -m pytest -q
	cd site && npm test
