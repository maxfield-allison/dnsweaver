#!/usr/bin/env python3
"""Capture protected DNS baselines before an explicitly authorized qualification."""

import argparse
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request


def save(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n")


def request(url, token, params=None, method="GET"):
    headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
    body = None
    if params:
        encoded = urllib.parse.urlencode(params)
        if method == "GET":
            url += "?" + encoded
        else:
            body = encoded.encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            value = json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError("HTTP status " + str(error.code)) from None
    except (urllib.error.URLError, OSError):
        # Transport errors may contain query credentials; never render them.
        raise RuntimeError("provider transport unavailable") from None
    if value.get("success") is False or value.get("status") in ("error", "invalid-token"):
        raise RuntimeError("provider rejected request")
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--zone", required=True)
    parser.add_argument("--local-zone", required=True)
    parser.add_argument("--technitium-url", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--create-local-zone", action="store_true")
    parser.add_argument("--catalog-zone")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    cf_token = Path(os.environ["CLOUDFLARE_TOKEN_FILE"]).read_text().strip()
    tech_token = Path(os.environ["TECHNITIUM_TOKEN_FILE"]).read_text().strip()
    cf_url = "https://api.cloudflare.com/client/v4"
    zones = request(cf_url + "/zones", cf_token, {"name": args.zone})["result"]
    if len(zones) != 1 or zones[0]["name"] != args.zone:
        raise RuntimeError("expected exactly one authorized Cloudflare zone")
    zone = zones[0]
    save(args.output / "cloudflare-zone.json", zone)
    records = []
    page = 1
    while True:
        listing = request(cf_url + "/zones/" + zone["id"] + "/dns_records", cf_token, {"page": page, "per_page": 100})
        batch = listing["result"]
        records.extend(batch)
        if page >= listing.get("result_info", {}).get("total_pages", page) or len(batch) < 100:
            break
        page += 1
    if not (args.output / "cloudflare-baseline.json").exists():
        save(args.output / "cloudflare-baseline.json", records)
    usage = request(cf_url + "/zones/" + zone["id"] + "/dns_records/usage", cf_token)
    save(args.output / "cloudflare-usage.json", usage)
    tech_url = args.technitium_url.rstrip("/")
    params = {"token": tech_token}
    tech_zones = request(tech_url + "/api/zones/list", tech_token, params)["response"]["zones"]
    session = request(tech_url + "/api/user/session/get", tech_token, params)
    existing = [z for z in tech_zones if z["name"] == args.local_zone]
    if not existing and args.create_local_zone:
        creation = {"token": tech_token, "zone": args.local_zone, "type": "Primary"}
        if args.catalog_zone:
            creation["catalog"] = args.catalog_zone
        request(tech_url + "/api/zones/create", tech_token,
                creation, method="POST")
        existing = [{"name": args.local_zone, "type": "Primary", "created_by_qualification": True}]
    if existing:
        save(args.output / "technitium-zone.json", existing[0])
        baseline = request(tech_url + "/api/zones/records/get", tech_token,
                           {"token": tech_token, "zone": args.local_zone, "domain": args.local_zone, "listZone": "true"})["response"]["records"]
        if not (args.output / "technitium-baseline.json").exists():
            save(args.output / "technitium-baseline.json", baseline)
    else:
        baseline = []
    summary = {"cloudflare": {"zone": zone["name"], "zone_id": zone["id"], "status": zone["status"],
                              "created_on": zone.get("created_on"), "records": len(records), "usage": usage.get("result")},
               "technitium": {"url": tech_url, "zone": args.local_zone, "exists": bool(existing),
                              "zone_type": existing[0].get("type") if existing else None,
                              "records": len(baseline), "created": bool(existing and existing[0].get("created_by_qualification")),
                              "server": session.get("info", {}).get("dnsServerDomain"),
                              "version": session.get("info", {}).get("version"),
                              "zone_permissions": session.get("info", {}).get("permissions", {}).get("Zones")}}
    save(args.output / "preflight.json", summary)
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, KeyError, OSError) as error:
        # Only explicitly sanitized exceptions may enter stdout/stderr.
        message = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        print("Qualification preflight failed: " + message, file=sys.stderr)
        sys.exit(1)
