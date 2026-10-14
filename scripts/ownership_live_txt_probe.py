#!/usr/bin/env python3
"""Isolate a rejected Cloudflare TXT page in an explicitly authorized empty zone."""

import argparse
import hashlib
import json
from pathlib import Path
import urllib.error
import urllib.request

from ownership_live_preflight import save


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    args = parser.parse_args()
    cfg = json.loads(args.config.read_text())
    if cfg["Provider"] != "cloudflare" or not cfg["Prefix"].startswith("qa-"):
        raise RuntimeError("probe requires an explicitly scoped Cloudflare QA configuration")
    token = Path(cfg["TokenFile"]).read_text().strip()
    base = "https://api.cloudflare.com/client/v4/zones/" + cfg["ZoneID"] + "/dns_records"

    def request(method, path="", body=None):
        payload = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(base + path, data=payload, method=method,
                                     headers={"Authorization": "Bearer " + token,
                                              "Content-Type": "application/json"})
        try:
            response = urllib.request.urlopen(req, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        except (urllib.error.URLError, OSError):
            raise RuntimeError("probe transport unavailable") from None
        with response:
            result = json.load(response)
            return response.status, result

    _, initial = request("GET", "?per_page=100")
    if not initial.get("success") or initial.get("result"):
        raise RuntimeError("probe requires an empty full DNS inventory")
    pages = json.loads((Path(cfg["Output"]) / "prepared-pages.json").read_text())
    page = pages[0]
    value, name = page["Target"], page["Hostname"]
    if not name.endswith("." + cfg["Zone"]) or not name.startswith("_dw-p."):
        raise RuntimeError("unexpected prepared ownership page")
    variants = [
        ("encoded-original-raw", name, value),
        ("encoded-neutral-raw", "wire-probe." + cfg["Prefix"] + "." + cfg["Zone"], value),
        ("encoded-original-chunks", name, " ".join(json.dumps(value[i:i + 255]) for i in range(0, len(value), 255))),
        ("repetitive-original-raw", name, "dnsweaver-page=v1:" + "a" * (3500 - len("dnsweaver-page=v1:"))),
        ("encoded-original-short", name, value[:2000]),
        ("encoded-original-2040", name, value[:2040]),
        ("encoded-original-2048", name, value[:2048]),
        ("encoded-original-2050", name, value[:2050]),
        ("encoded-original-2500", name, value[:2500]),
        ("hex-original-3500", name, "".join(hashlib.sha256(str(i).encode()).hexdigest() for i in range(55))[:3500]),
    ]
    receipts = []
    for label, hostname, content in variants:
        body = {"type": "TXT", "name": hostname, "content": content, "ttl": 300, "proxied": False}
        status, result = request("POST", body=body)
        receipt = {"case": label, "name": hostname, "content_bytes": len(content),
                   "http_status": status, "success": result.get("success"),
                   "error_codes": [e.get("code") for e in result.get("errors", [])]}
        receipts.append(receipt)
        save(Path(cfg["Output"]) / "representation-probes.json", receipts)
        if result.get("success"):
            record_id = result["result"]["id"]
            _, deleted = request("DELETE", "/" + record_id)
            if not deleted.get("success"):
                raise RuntimeError("probe cleanup failed; do not retry mutations")
        print(json.dumps(receipt))
    _, final = request("GET", "?per_page=100")
    save(Path(cfg["Output"]) / "probe-after-cleanup.json", final)
    if not final.get("success") or final.get("result"):
        raise RuntimeError("probe inventory did not return to empty")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, KeyError, OSError, ValueError) as error:
        # Credential-bearing transport details are intentionally suppressed.
        message = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        raise SystemExit("TXT representation probe failed: " + message) from None
