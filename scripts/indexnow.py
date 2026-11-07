#!/usr/bin/env python3
"""Tell IndexNow search engines (Bing, Yandex, and others) about every sitemap URL.

Runs after a Pages deployment. The key file is published by scripts/build-site.py.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen
from xml.etree import ElementTree

ROOT = Path(__file__).resolve().parents[1]
SITE = ROOT / "docs" / "site"
BASE_URL = "https://mikeo7.github.io/HarborBuddy/"
ENDPOINT = "https://api.indexnow.org/indexnow"


def main() -> int:
    keys = [path.stem for path in SITE.glob("*.txt") if len(path.stem) == 32 and path.read_text().strip() == path.stem]
    if len(keys) != 1:
        print("expected exactly one IndexNow key file in docs/site", file=sys.stderr)
        return 1
    namespace = {"sm": "http://www.sitemaps.org/schemas/sitemap/0.9"}
    tree = ElementTree.parse(SITE / "sitemap.xml")
    urls = [loc.text or "" for loc in tree.findall("sm:url/sm:loc", namespace)]
    payload = {
        "host": "mikeo7.github.io",
        "key": keys[0],
        "keyLocation": f"{BASE_URL}{keys[0]}.txt",
        "urlList": urls,
    }
    request = Request(
        ENDPOINT,
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json; charset=utf-8"},
        method="POST",
    )
    try:
        with urlopen(request, timeout=30) as response:
            print(f"IndexNow accepted {len(urls)} URLs (HTTP {response.status})")
    except HTTPError as error:
        print(f"IndexNow rejected the submission: HTTP {error.code}", file=sys.stderr)
        return 1
    except URLError as error:
        print(f"IndexNow request failed: {error.reason}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
