"""Build the public product site without application or publishing credentials."""

from __future__ import annotations

import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUTPUT = ROOT / "build" / "website"


def main() -> None:
    if OUTPUT.exists():
        shutil.rmtree(OUTPUT)
    shutil.copytree(ROOT / "website", OUTPUT)
    print("Built build/website; static files only, no application build")


if __name__ == "__main__":
    main()
