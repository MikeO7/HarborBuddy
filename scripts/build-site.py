#!/usr/bin/env python3
"""Build the static GitHub Pages site in docs/site from site/pages.

Each page source starts with a JSON header inside an HTML comment:

    <!--page {"title": "...", "description": "...", ...} -->

The rest of the file is the page body. The build wraps it in the shared head,
header, and footer, adds structured data, and writes sitemap.xml. Run with
--check to fail when docs/site is out of date.
"""

from __future__ import annotations

import html
import json
import re
import sys
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "site" / "pages"
OUTPUT = ROOT / "docs" / "site"
BASE_URL = "https://mikeo7.github.io/HarborBuddy/"
PREFIX = "/HarborBuddy/"
REPO = "https://github.com/MikeO7/HarborBuddy"
VERSION = "0.1.0"
SOCIAL_IMAGE = BASE_URL + "assets/harborbuddy-social.png"
SOCIAL_ALT = "HarborBuddy guiding Docker containers through an update and rollback path"
HEADER_RE = re.compile(r"\A<!--page\s+(\{.*?\})\s*-->\n", re.S)
AI_CRAWLERS = (
    "OAI-SearchBot", "GPTBot", "ChatGPT-User", "Claude-SearchBot", "ClaudeBot", "Claude-User",
    "PerplexityBot", "Perplexity-User", "Google-Extended", "Applebot-Extended", "Bingbot", "CCBot",
)
PERSON_ID = "https://github.com/MikeO7#person"
SOFTWARE_ID = BASE_URL + "#software"
WEBSITE_ID = BASE_URL + "#website"

BRAND_MARK = (
    '<svg class="brand-mark" viewBox="0 0 48 48" aria-hidden="true"><path d="M24 4v28M14 13h20'
    "M11 24c0 12 5 18 13 20 8-2 13-8 13-20M6 27c4 2 7 2 10 0M32 27c3 2 6 2 10 0\"/></svg>"
)
NAV = [
    ("docs/", "Docs"),
    ("compare/", "Compare"),
    ("compare/watchtower/", "Watchtower alternative"),
    ("#faq", "FAQ"),
]
SECTIONS = {"docs/": "Docs", "compare/": "Compare"}


def load_pages() -> list[dict]:
    pages = []
    for path in sorted(SOURCE.rglob("*.html")):
        text = path.read_text(encoding="utf-8")
        match = HEADER_RE.match(text)
        if not match:
            raise SystemExit(f"{path.relative_to(ROOT)}: missing <!--page {{...}} --> header")
        meta = json.loads(match.group(1))
        relative = path.relative_to(SOURCE).with_suffix("").as_posix()
        if relative == "index":
            meta["path"] = ""
        elif relative == "404":
            meta["path"] = "404.html"
        else:
            meta["path"] = relative.removesuffix("/index") + "/"
        meta["body"] = text[match.end():]
        pages.append(meta)
    return pages


def strip_tags(value: str) -> str:
    return " ".join(html.unescape(re.sub(r"<[^>]+>", "", value)).split())


def esc(value: str) -> str:
    return html.escape(value, quote=True)


def render_faq(items: list[list[str]]) -> str:
    rows = [
        f"          <details>\n            <summary>{esc(q)}</summary>\n            <p>{a}</p>\n          </details>"
        for q, a in items
    ]
    return '<div class="faq-list">\n' + "\n".join(rows) + "\n        </div>"


def breadcrumbs(page: dict) -> list[tuple[str, str]]:
    crumbs = [("HarborBuddy", "")]
    for section, name in SECTIONS.items():
        if page["path"].startswith(section) and page["path"] != section:
            crumbs.append((name, section))
    crumbs.append((page.get("crumb", page["title"]), page["path"]))
    return crumbs


def visible_breadcrumbs(page: dict) -> str:
    items = []
    crumbs = breadcrumbs(page)
    for index, (name, path) in enumerate(crumbs):
        if index == len(crumbs) - 1:
            items.append(f'<li aria-current="page">{esc(name)}</li>')
        else:
            items.append(f'<li><a href="{PREFIX}{path}">{esc(name)}</a></li>')
    narrow = " narrow" if page.get("kind") == "article" else ""
    return f'<nav class="breadcrumbs{narrow}" aria-label="Breadcrumb"><ol>{"".join(items)}</ol></nav>'


def structured_data(page: dict, pages: list[dict]) -> str:
    url = BASE_URL + page["path"]
    graph: list[dict] = [
        {"@type": "Person", "@id": PERSON_ID, "name": "MikeO7", "url": "https://github.com/MikeO7"},
        {
            "@type": "WebSite",
            "@id": WEBSITE_ID,
            "url": BASE_URL,
            "name": "HarborBuddy",
            "description": "Documentation and comparisons for HarborBuddy, a self-hosted Docker container updater with rollback.",
            "inLanguage": "en",
            "publisher": {"@id": PERSON_ID},
        },
    ]
    kind = page.get("kind", "article")
    web_page = {
        "@type": "TechArticle" if kind == "docs" else "WebPage",
        "@id": url + "#webpage",
        "url": url,
        "name": page["title"],
        "headline": page.get("headline", page["title"]),
        "description": page["description"],
        "isPartOf": {"@id": WEBSITE_ID},
        "about": {"@id": SOFTWARE_ID},
        "inLanguage": "en",
        "dateModified": page["updated"],
        "author": {"@id": PERSON_ID},
    }
    if kind == "home":
        web_page["mainEntity"] = {"@id": SOFTWARE_ID}
        del web_page["headline"]
    graph.append(web_page)
    if kind == "home":
        graph.append(page["software"] | {"@type": "SoftwareApplication", "@id": SOFTWARE_ID})
        graph.append(
            {
                "@type": "SoftwareSourceCode",
                "@id": REPO + "#source",
                "name": "HarborBuddy source code",
                "codeRepository": REPO,
                "programmingLanguage": "Go",
                "runtimePlatform": "Docker Engine, Linux",
                "license": "https://polyformproject.org/licenses/noncommercial/1.0.0/",
                "author": {"@id": PERSON_ID},
                "isPartOf": {"@id": SOFTWARE_ID},
            }
        )
    else:
        crumbs = [(name, BASE_URL + path) for name, path in breadcrumbs(page)]
        graph.append(
            {
                "@type": "BreadcrumbList",
                "@id": url + "#breadcrumb",
                "itemListElement": [
                    {"@type": "ListItem", "position": i, "name": name, "item": item}
                    for i, (name, item) in enumerate(crumbs, start=1)
                ],
            }
        )
        web_page["breadcrumb"] = {"@id": url + "#breadcrumb"}
    if page.get("faq"):
        graph.append(
            {
                "@type": "FAQPage",
                "@id": url + "#faq",
                "isPartOf": {"@id": url + "#webpage"},
                "mainEntity": [
                    {
                        "@type": "Question",
                        "name": q,
                        "acceptedAnswer": {"@type": "Answer", "text": strip_tags(a)},
                    }
                    for q, a in page["faq"]
                ],
            }
        )
    data = json.dumps({"@context": "https://schema.org", "@graph": graph}, indent=2, ensure_ascii=False)
    return data.replace("</", "<\\/")


def head(page: dict, pages: list[dict]) -> str:
    indexable = page["path"] != "404.html"
    url = BASE_URL + page["path"]
    social_title = page.get("social_title", page["title"])
    lines = [
        '<meta charset="utf-8">',
        '<meta name="viewport" content="width=device-width, initial-scale=1">',
        f"<title>{esc(page['title'])}</title>",
        f'<meta name="description" content="{esc(page["description"])}">',
        '<meta name="theme-color" content="#071923">',
        f'<link rel="stylesheet" href="{PREFIX}styles.css">',
        f'<link rel="icon" href="{PREFIX}assets/favicon.svg" type="image/svg+xml">',
        f'<link rel="apple-touch-icon" href="{PREFIX}assets/apple-touch-icon.png">',
    ]
    if not indexable:
        lines.insert(4, '<meta name="robots" content="noindex,follow">')
        return "\n    ".join(lines)
    lines[4:4] = [
        '<meta name="robots" content="index,follow,max-image-preview:large,max-snippet:-1">',
        f'<link rel="canonical" href="{url}">',
    ]
    lines += [
        f'<link rel="alternate" href="{PREFIX}llms.txt" type="text/plain" title="HarborBuddy facts for language models">',
        f'<link rel="alternate" type="application/atom+xml" title="HarborBuddy releases" href="{REPO}/releases.atom">',
        '<meta property="og:type" content="website">' if page.get("kind") == "home" else '<meta property="og:type" content="article">',
        '<meta property="og:locale" content="en_US">',
        '<meta property="og:site_name" content="HarborBuddy">',
        f'<meta property="og:title" content="{esc(social_title)}">',
        f'<meta property="og:description" content="{esc(page["description"])}">',
        f'<meta property="og:url" content="{url}">',
        f'<meta property="og:image" content="{SOCIAL_IMAGE}">',
        '<meta property="og:image:type" content="image/png">',
        '<meta property="og:image:width" content="1200">',
        '<meta property="og:image:height" content="630">',
        f'<meta property="og:image:alt" content="{SOCIAL_ALT}">',
        '<meta name="twitter:card" content="summary_large_image">',
        f'<meta name="twitter:title" content="{esc(social_title)}">',
        f'<meta name="twitter:description" content="{esc(page["description"])}">',
        f'<meta name="twitter:image" content="{SOCIAL_IMAGE}">',
        f'<meta name="twitter:image:alt" content="{SOCIAL_ALT}">',
        f'<script type="application/ld+json">\n{structured_data(page, pages)}\n    </script>',
    ]
    return "\n    ".join(lines)


def header(page: dict) -> str:
    links = []
    for target, label in NAV:
        href = (PREFIX + target) if not target.startswith("#") else PREFIX + target
        exact = any(page["path"] == other for other, _ in NAV)
        current = page["path"] == target or (
            not exact and target in SECTIONS and page["path"].startswith(target)
        )
        attr = ' aria-current="page"' if current else ""
        links.append(f'<a href="{href}"{attr}>{label}</a>')
    links.append(f'<a class="nav-cta" href="{REPO}">GitHub</a>')
    return f"""<a class="skip-link" href="#main">Skip to content</a>
    <header class="site-header">
      <a class="brand" href="{PREFIX}" aria-label="HarborBuddy home">{BRAND_MARK}<span>HarborBuddy</span></a>
      <a class="release-chip" href="{REPO}/releases/tag/v{VERSION}">v{VERSION}</a>
      <nav aria-label="Main navigation">
        {"\n        ".join(links)}
      </nav>
    </header>"""


def docs_sidebar(page: dict, pages: list[dict]) -> str:
    docs = sorted((p for p in pages if p["path"].startswith("docs/")), key=lambda p: p.get("order", 99))
    items = []
    for doc in docs:
        attr = ' aria-current="page"' if doc is page else ""
        items.append(f'<li><a href="{PREFIX}{doc["path"]}"{attr}>{esc(doc.get("crumb", doc["title"]))}</a></li>')
    return (
        '<nav class="docs-nav" aria-label="Documentation">\n          <p class="docs-nav-title">Documentation</p>\n'
        '          <ul>\n            ' + "\n            ".join(items) + "\n          </ul>\n        </nav>"
    )


def footer() -> str:
    def group(title: str, links: list[tuple[str, str]]) -> str:
        anchors = "".join(f'<li><a href="{href}">{label}</a></li>' for href, label in links)
        return f'<div><p class="footer-title">{title}</p><ul>{anchors}</ul></div>'

    groups = [
        group("Product", [(PREFIX + "docs/quick-start/", "Quick start"), (PREFIX + "docs/", "Documentation"),
                          (PREFIX + "docs/configuration/", "Configuration reference"), (PREFIX + "#faq", "FAQ")]),
        group("Compare", [(PREFIX + "compare/", "Docker update tools compared"),
                          (PREFIX + "compare/watchtower/", "Watchtower alternative"),
                          (PREFIX + "compare/watchtower/#migrate", "Migrate from Watchtower"),
                          (PREFIX + "compare/whats-up-docker/", "HarborBuddy vs What's Up Docker"),
                          (PREFIX + "compare/diun/", "HarborBuddy vs Diun")]),
        group("Project", [(REPO, "Source code"), (REPO + "/releases", "Releases"),
                          (REPO + "/blob/main/CHANGELOG.md", "Changelog"), (REPO + "/issues", "Issues"),
                          (REPO + "/security/policy", "Security policy")]),
    ]
    return f"""<footer class="site-footer">
      <div class="footer-brand">
        <a class="brand" href="{PREFIX}">{BRAND_MARK}<span>HarborBuddy</span></a>
        <p>Careful, self-hosted container updates for standalone Docker Engine. Built by <a href="https://github.com/MikeO7">MikeO7</a>.</p>
        <p class="footer-license">Noncommercial source-available software under the <a rel="license" href="{REPO}/blob/main/LICENSE">PolyForm Noncommercial License 1.0.0</a>.</p>
      </div>
      <div class="footer-groups">
        {"\n        ".join(groups)}
      </div>
    </footer>"""


def render_body(page: dict) -> str:
    body = page["body"]
    if page.get("faq"):
        body = body.replace("{{faq}}", render_faq(page["faq"]))
    body = body.replace("{{base}}", PREFIX).replace("{{repo}}", REPO).replace("{{version}}", VERSION)
    leftover = re.search(r"\{\{\w+\}\}", body)
    if leftover:
        raise SystemExit(f"{page['path'] or 'home'}: unknown placeholder {leftover.group(0)}")
    return body


def render(page: dict, pages: list[dict]) -> str:
    body = render_body(page)
    if page.get("kind") in {"docs", "article"}:
        body = "          " + visible_breadcrumbs(page) + "\n" + body
    if page.get("kind") == "docs":
        body = f"""<div class="docs-layout">
        {docs_sidebar(page, pages)}
        <article class="prose">
{body.rstrip()}
        </article>
      </div>"""
    main_class = f' class="{page["main_class"]}"' if page.get("main_class") else ""
    return f"""<!doctype html>
<html lang="en">
  <head>
    {head(page, pages)}
  </head>
  <body>
    {header(page)}

    <main id="main"{main_class}>
      {body.strip()}
    </main>

    {footer()}
  </body>
</html>
"""


def sitemap(pages: list[dict]) -> str:
    entries = "".join(
        f"  <url>\n    <loc>{BASE_URL + p['path']}</loc>\n    <lastmod>{p['updated']}</lastmod>\n  </url>\n"
        for p in sorted(pages, key=lambda p: p["path"])
        if p["path"] != "404.html"
    )
    return f'<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n{entries}</urlset>\n'


class TextExtractor(HTMLParser):
    """Turn page bodies into Markdown-like text for llms-full.txt."""

    SKIP = {"svg", "figure", "nav", "script", "style"}
    SKIP_CLASSES = {"step-number", "route-number", "visually-hidden"}
    BLOCKS = {"div": "\n", "article": "\n", "section": "\n", "header": "\n", "span": "",
              "p": "\n\n", "h1": "\n\n# ", "h2": "\n\n## ", "h3": "\n\n### ", "tr": "\n", "dt": "\n- **",
              "pre": "\n\n```\n", "ul": "\n", "ol": "\n", "details": "\n", "summary": "\n\n### "}

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.out: list[str] = []
        self.skip = 0
        self.skip_tag = ""
        self.pre = False
        self.cells = 0
        self.href = ""
        self.ordered: list[int] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if self.skip:
            self.skip += tag == self.skip_tag
            return
        values = dict(attrs)
        classes = set((values.get("class") or "").split())
        if tag in self.SKIP or values.get("aria-hidden") == "true" or classes & self.SKIP_CLASSES:
            self.skip, self.skip_tag = 1, tag
            return
        if "feature-label" in classes or "eyebrow" in classes:
            self.out.append("\n\n")
        if tag in self.BLOCKS:
            self.out.append(self.BLOCKS[tag])
        if tag == "pre":
            self.pre = True
        elif tag == "ol":
            self.ordered.append(0)
        elif tag == "ul":
            self.ordered.append(-1)
        elif tag == "li":
            depth = self.ordered[-1] if self.ordered else -1
            if depth >= 0:
                self.ordered[-1] += 1
                self.out.append(f"\n{self.ordered[-1]}. ")
            else:
                self.out.append("\n- ")
        elif tag in {"td", "th"}:
            self.out.append(" | " if self.cells else "| ")
            self.cells += 1
        elif tag == "tr":
            self.cells = 0
        elif tag == "dd":
            self.out.append("**: ")
        elif tag == "code" and not self.pre:
            self.out.append("`")
        elif tag == "strong":
            self.out.append("**")
        elif tag == "a":
            href = values.get("href") or ""
            self.href = BASE_URL + href.removeprefix(PREFIX) if href.startswith(PREFIX) else href

    def handle_endtag(self, tag: str) -> None:
        if self.skip:
            self.skip -= tag == self.skip_tag
            return
        if tag == "pre":
            self.pre = False
            self.out.append("\n```")
        elif tag in {"ol", "ul"} and self.ordered:
            self.ordered.pop()
        elif tag == "tr":
            self.out.append(" |")
        elif tag == "code" and not self.pre:
            self.out.append("`")
        elif tag == "strong":
            self.out.append("**")
        elif tag == "a" and self.href.startswith("http"):
            self.out.append(f" ({self.href})")
            self.href = ""

    def handle_data(self, data: str) -> None:
        if self.skip:
            return
        self.out.append(data if self.pre else re.sub(r"\s+", " ", data))

    def text(self) -> str:
        joined = "".join(self.out)
        joined = re.sub(r"[ \t]+\n", "\n", joined)
        joined = re.sub(r"\n(?:[ \t]*\n)+", "\n\n", joined)
        return joined.strip()


def llms_full(pages: list[dict]) -> str:
    order = sorted(
        (p for p in pages if p["path"] != "404.html"),
        key=lambda p: (p["path"] != "", not p["path"].startswith("docs/"), p.get("order", 50), p["path"]),
    )
    parts = [
        "# HarborBuddy: full documentation\n\n"
        "> HarborBuddy is a self-hosted Docker container updater with health checks and automatic rollback. "
        f"This file contains the full text of every page on {BASE_URL} for language models. "
        f"Version {VERSION}. Source: {REPO}"
    ]
    for page in order:
        extractor = TextExtractor()
        extractor.feed(render_body(page))
        parts.append(f"---\n\nURL: {BASE_URL + page['path']}\nUpdated: {page['updated']}\n\n{extractor.text()}")
    return "\n\n".join(parts) + "\n"


def outputs() -> dict[Path, str]:
    pages = load_pages()
    result: dict[Path, str] = {}
    for page in pages:
        target = OUTPUT / ("404.html" if page["path"] == "404.html" else page["path"] + "index.html")
        result[target] = render(page, pages)
    result[OUTPUT / "sitemap.xml"] = sitemap(pages)
    crawlers = "".join(f"User-agent: {agent}\n" for agent in AI_CRAWLERS)
    result[OUTPUT / "robots.txt"] = (
        f"User-agent: *\nAllow: /\n\n{crawlers}Allow: /\n\nSitemap: {BASE_URL}sitemap.xml\n"
    )
    result[OUTPUT / "llms-full.txt"] = llms_full(pages)
    return result


def main(argv: list[str]) -> int:
    check = "--check" in argv
    stale = []
    for path, content in outputs().items():
        current = path.read_text(encoding="utf-8") if path.exists() else None
        if current == content:
            continue
        if check:
            stale.append(path.relative_to(ROOT).as_posix())
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
    if stale:
        print("docs/site is out of date; run scripts/build-site.py:\n- " + "\n- ".join(stale), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
