"""Build-time configuration for product and documentation analytics."""

from __future__ import annotations

import html
import json
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def tracker_tag(surface: str, *, preview: bool = False) -> str:
    """Return a hostname-restricted script, or omit collection for a preview."""
    if preview:
        return ""
    config = json.loads((ROOT / "website" / "analytics.json").read_text())
    identifier = config["website_id"]
    if not identifier:
        raise ValueError(
            "Register the product in Umami before a production build; use --preview locally"
        )
    if str(uuid.UUID(identifier)) != identifier:
        raise ValueError("Umami Website IDs must be canonical UUIDs")
    prefix = config["docs_prefix"] if surface == "docs" else "/"
    attributes = {
        "src": prefix + "assets/javascripts/analytics.js",
        "data-product": config["product"],
        "data-website-id": identifier,
        "data-app-host": config["app_host"],
        "data-docs-host": config["docs_host"],
        "data-docs-prefix": config["docs_prefix"],
        "data-surface": surface,
        "data-utm-values": json.dumps(config["campaign_values"], separators=(",", ":")),
    }
    rendered = " ".join(
        f'{key}="{html.escape(value, quote=True)}"' for key, value in attributes.items()
    )
    return f"<script defer {rendered}></script>"
