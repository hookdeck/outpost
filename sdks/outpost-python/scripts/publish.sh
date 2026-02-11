#!/usr/bin/env bash
# README.md is the source of truth. For PyPI we need absolute links, so we
# generate README-PYPI.md and point pyproject at it only for this build.
export POETRY_PYPI_TOKEN_PYPI=${PYPI_TOKEN}

poetry run python scripts/prepare_readme.py
# Point pyproject at the link-rewritten readme for the publish build
sed -i.bak 's/readme = "README.md"/readme = "README-PYPI.md"/' pyproject.toml
poetry publish --build --skip-existing
rm -f pyproject.toml.bak
