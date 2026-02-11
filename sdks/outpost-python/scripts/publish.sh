#!/usr/bin/env bash
# pyproject.toml points at README-PYPI.md (Speakeasy default). prepare_readme.py
# overwrites it with link-rewritten content from README.md for PyPI.
export POETRY_PYPI_TOKEN_PYPI=${PYPI_TOKEN}

poetry run python scripts/prepare_readme.py
poetry publish --build --skip-existing
