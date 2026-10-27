"""Build the public product site without application or publishing credentials."""

from __future__ import annotations

import argparse
import shutil
from pathlib import Path

from site_config import tracker_tag

ROOT = Path(__file__).resolve().parent.parent
OUTPUT = ROOT / "build" / "website"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--preview", action="store_true", help="build without analytics"
    )
    args = parser.parse_args()
    analytics = tracker_tag("site", preview=args.preview)
    if OUTPUT.exists():
        shutil.rmtree(OUTPUT)
    shutil.copytree(ROOT / "website", OUTPUT)
    (OUTPUT / "analytics.json").unlink()
    for page in OUTPUT.rglob("*.html"):
        contents = page.read_text()
        if contents.count("</head>") != 1:
            raise ValueError(
                f"Expected one document head in {page.relative_to(OUTPUT)}"
            )
        page.write_text(contents.replace("</head>", analytics + "</head>"))
    print("Built build/website; static files only, no application build")


if __name__ == "__main__":
    main()
