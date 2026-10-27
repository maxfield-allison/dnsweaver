"""Check production registration and HTML serialization boundaries."""

import json
import tempfile
import unittest
from html.parser import HTMLParser
from pathlib import Path
from unittest.mock import patch

import site_config


class Script(HTMLParser):
    attributes = None

    def handle_starttag(self, tag, attrs):
        if tag == "script":
            self.attributes = dict(attrs)


class AnalyticsConfigTests(unittest.TestCase):
    def test_preview_never_reads_registration(self):
        with patch.object(site_config, "ROOT", Path("/missing-registration")):
            self.assertEqual(site_config.tracker_tag("site", preview=True), "")

    def test_production_rejects_missing_registration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "website").mkdir()
            (root / "website/analytics.json").write_text('{"website_id":""}')
            with (
                patch.object(site_config, "ROOT", root),
                self.assertRaisesRegex(ValueError, "Register the product"),
            ):
                site_config.tracker_tag("site")

    def test_browser_can_read_campaign_json(self):
        parsed = Script()
        parsed.feed(site_config.tracker_tag("site"))
        values = json.loads(parsed.attributes["data-utm-values"])
        self.assertIn("bluesky", values["utm_source"])
        self.assertEqual(parsed.attributes["src"], "/assets/javascripts/analytics.js")
        self.assertEqual(parsed.attributes["data-surface"], "site")


if __name__ == "__main__":
    unittest.main()
