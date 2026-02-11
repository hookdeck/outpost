"""
Override Speakeasy-generated pyproject.toml to use README.md as the package readme.

Speakeasy emits readme = "README-PYPI.md" but does not generate that file. We use
README.md as the single source of truth for the SDK; this script patches pyproject.toml
after generation so build/test work. At publish time, prepare_readme.py creates
README-PYPI.md and publish.sh builds with it.
"""
import sys

PYPROJECT = "pyproject.toml"
OLD = 'readme = "README-PYPI.md"'
NEW = 'readme = "README.md"'


def main() -> int:
    try:
        with open(PYPROJECT, "r", encoding="utf-8") as f:
            content = f.read()
    except OSError as e:
        print(f"Failed to read {PYPROJECT}: {e}", file=sys.stderr)
        return 1

    if OLD not in content:
        if NEW in content:
            return 0  # Already patched
        print(f"Neither {OLD!r} nor {NEW!r} found in {PYPROJECT}", file=sys.stderr)
        return 1

    content = content.replace(OLD, NEW, 1)
    try:
        with open(PYPROJECT, "w", encoding="utf-8") as f:
            f.write(content)
    except OSError as e:
        print(f"Failed to write {PYPROJECT}: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
