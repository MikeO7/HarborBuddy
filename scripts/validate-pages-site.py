#!/usr/bin/env python3
"""Validate the static GitHub Pages artifact without third-party packages."""

from __future__ import annotations

import json
import re
import struct
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlparse
from xml.etree import ElementTree

ROOT = Path(__file__).resolve().parents[1]
SITE = ROOT / "docs" / "site"
BASE_URL = "https://mikeo7.github.io/HarborBuddy/"
PROJECT_PATH = "/HarborBuddy/"
ALLOWED_SUFFIXES = {".html", ".css", ".txt", ".xml", ".png", ".svg"}
REQUIRED_SOCIAL_META = {
    "og:type",
    "og:site_name",
    "og:title",
    "og:description",
    "og:url",
    "og:image",
    "og:image:type",
    "og:image:width",
    "og:image:height",
    "og:image:alt",
    "twitter:card",
    "twitter:title",
    "twitter:description",
    "twitter:image",
    "twitter:image:alt",
}


class PageParser(HTMLParser):
    """Collect the page structure needed by the site checks."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.capture_tag = ""
        self.capture_text: list[str] = []
        self.title = ""
        self.h1_text: list[str] = []
        self.summaries: list[str] = []
        self.ids: list[str] = []
        self.links: list[str] = []
        self.meta: dict[str, str] = {}
        self.canonical: list[str] = []
        self.json_ld: list[object] = []
        self.json_errors: list[str] = []
        self.has_nav = False
        self.has_footer = False

    @staticmethod
    def attrs_dict(attrs: list[tuple[str, str | None]]) -> dict[str, str]:
        return {key: value or "" for key, value in attrs}

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        attributes = self.attrs_dict(attrs)
        if tag in {"title", "h1", "summary"} or (
            tag == "script" and attributes.get("type") == "application/ld+json"
        ):
            self.capture_tag = tag
            self.capture_text = []

        element_id = attributes.get("id")
        if element_id:
            self.ids.append(element_id)
        if tag == "nav":
            self.has_nav = True
        if tag == "footer":
            self.has_footer = True
        if tag == "meta":
            key = attributes.get("name") or attributes.get("property")
            if key:
                self.meta[key] = attributes.get("content", "")
        if tag == "link":
            href = attributes.get("href")
            if href:
                self.links.append(href)
            if "canonical" in attributes.get("rel", "").split() and href:
                self.canonical.append(href)
        if tag in {"a", "img", "script", "source"}:
            for attribute in ("href", "src"):
                value = attributes.get(attribute)
                if value:
                    self.links.append(value)

    def handle_data(self, data: str) -> None:
        if self.capture_tag:
            self.capture_text.append(data)

    def handle_endtag(self, tag: str) -> None:
        if tag != self.capture_tag:
            return
        text = " ".join("".join(self.capture_text).split())
        if tag == "title":
            self.title = text
        elif tag == "h1":
            self.h1_text.append(text)
        elif tag == "summary":
            self.summaries.append(text)
        else:
            raw = "".join(self.capture_text).strip()
            try:
                self.json_ld.append(json.loads(raw))
            except json.JSONDecodeError as error:
                self.json_errors.append(str(error))
        self.capture_tag = ""
        self.capture_text = []


def is_indexable(parser: PageParser) -> bool:
    return "noindex" not in parser.meta.get("robots", "").lower()


def expected_canonical(path: Path) -> str:
    relative = path.relative_to(SITE).as_posix()
    return BASE_URL if relative == "index.html" else BASE_URL + relative


def local_target(source: Path, raw_url: str) -> tuple[Path | None, str]:
    if not raw_url or raw_url.startswith(("mailto:", "tel:", "data:", "javascript:")):
        return None, ""
    parsed = urlparse(raw_url)
    if parsed.scheme or parsed.netloc:
        if not raw_url.startswith(BASE_URL):
            return None, ""
        relative = unquote(parsed.path.removeprefix(PROJECT_PATH))
        target = SITE / relative
    elif parsed.path.startswith(PROJECT_PATH):
        target = SITE / unquote(parsed.path.removeprefix(PROJECT_PATH))
    elif parsed.path.startswith("/"):
        return None, ""
    else:
        target = source if not parsed.path else source.parent / unquote(parsed.path)

    if parsed.path.endswith("/"):
        target = target / "index.html"
    return target.resolve(), unquote(parsed.fragment)


def find_faq_questions(value: object) -> list[str]:
    found: list[str] = []
    if isinstance(value, dict):
        if value.get("@type") == "FAQPage":
            for question in value.get("mainEntity", []):
                if isinstance(question, dict) and isinstance(question.get("name"), str):
                    found.append(question["name"])
        for nested in value.values():
            found.extend(find_faq_questions(nested))
    elif isinstance(value, list):
        for nested in value:
            found.extend(find_faq_questions(nested))
    return found


def parse_pages(errors: list[str]) -> dict[Path, PageParser]:
    pages: dict[Path, PageParser] = {}
    for path in sorted(SITE.rglob("*.html")):
        parser = PageParser()
        try:
            parser.feed(path.read_text(encoding="utf-8"))
            parser.close()
        except Exception as error:
            errors.append(f"{path.relative_to(ROOT)}: HTML parse failed: {error}")
            continue
        pages[path.resolve()] = parser
    return pages


def validate_public_tree(errors: list[str]) -> None:
    if not SITE.is_dir():
        errors.append("docs/site does not exist")
        return
    for path in sorted(SITE.rglob("*")):
        if path.is_file() and (
            path.name.startswith(".") or path.suffix.lower() not in ALLOWED_SUFFIXES
        ):
            errors.append(f"{path.relative_to(ROOT)}: unexpected public artifact file")


def validate_pages(pages: dict[Path, PageParser], errors: list[str]) -> set[str]:
    indexable_urls: set[str] = set()
    for path, parser in pages.items():
        display = path.relative_to(ROOT)
        if not parser.title:
            errors.append(f"{display}: missing title")
        if len(parser.h1_text) != 1:
            errors.append(f"{display}: expected exactly one h1, found {len(parser.h1_text)}")
        duplicates = sorted({item for item in parser.ids if parser.ids.count(item) > 1})
        if duplicates:
            errors.append(f"{display}: duplicate ids: {', '.join(duplicates)}")
        if parser.json_errors:
            errors.append(f"{display}: invalid JSON-LD: {'; '.join(parser.json_errors)}")
        if not parser.has_nav:
            errors.append(f"{display}: missing navigation landmark")
        if not parser.has_footer:
            errors.append(f"{display}: missing footer landmark")

        if is_indexable(parser):
            if not parser.meta.get("description"):
                errors.append(f"{display}: missing meta description")
            if len(parser.canonical) != 1:
                errors.append(f"{display}: expected one canonical URL")
            elif parser.canonical[0] != expected_canonical(path):
                errors.append(f"{display}: incorrect canonical URL {parser.canonical[0]!r}")
            else:
                indexable_urls.add(parser.canonical[0])
            missing = sorted(key for key in REQUIRED_SOCIAL_META if not parser.meta.get(key))
            if missing:
                errors.append(f"{display}: missing social metadata: {', '.join(missing)}")

        if path.name == "index.html":
            structured: list[str] = []
            for block in parser.json_ld:
                structured.extend(find_faq_questions(block))
            if parser.summaries != structured:
                errors.append(f"{display}: visible FAQ questions do not match JSON-LD")

        for raw_url in parser.links:
            target, fragment = local_target(path, raw_url)
            if target is None:
                continue
            if not target.exists():
                errors.append(f"{display}: broken local reference {raw_url!r}")
                continue
            if fragment and target.suffix.lower() == ".html":
                target_parser = pages.get(target)
                if target_parser is None or fragment not in target_parser.ids:
                    errors.append(
                        f"{display}: missing fragment #{fragment} in {target.relative_to(ROOT)}"
                    )
    return indexable_urls


def validate_sitemap(indexable_urls: set[str], errors: list[str]) -> None:
    sitemap = SITE / "sitemap.xml"
    try:
        tree = ElementTree.parse(sitemap)
    except (OSError, ElementTree.ParseError) as error:
        errors.append(f"docs/site/sitemap.xml: invalid XML: {error}")
        return
    namespace = {"sm": "http://www.sitemaps.org/schemas/sitemap/0.9"}
    urls = [element.text or "" for element in tree.findall("sm:url/sm:loc", namespace)]
    if len(urls) != len(set(urls)):
        errors.append("docs/site/sitemap.xml: duplicate URLs")
    if set(urls) != indexable_urls:
        errors.append("docs/site/sitemap.xml: URLs do not match indexable pages")
    for entry in tree.findall("sm:url", namespace):
        if entry.find("sm:lastmod", namespace) is None:
            errors.append("docs/site/sitemap.xml: every URL requires lastmod")


def validate_llms(indexable_urls: set[str], errors: list[str]) -> None:
    text = (SITE / "llms.txt").read_text(encoding="utf-8")
    urls = set(re.findall(r"https://mikeo7\.github\.io/HarborBuddy/[^\s)]*", text))
    unknown = sorted(url.rstrip(".,") for url in urls if url.rstrip(".,") not in indexable_urls)
    if unknown:
        errors.append(f"docs/site/llms.txt: unknown public URLs: {', '.join(unknown)}")


def png_dimensions(path: Path) -> tuple[int, int]:
    with path.open("rb") as image:
        header = image.read(24)
    if len(header) < 24 or header[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError("not a PNG file")
    return struct.unpack(">II", header[16:24])


def validate_social_image(pages: dict[Path, PageParser], errors: list[str]) -> None:
    image = SITE / "assets" / "harborbuddy-social.png"
    try:
        width, height = png_dimensions(image)
    except (OSError, ValueError) as error:
        errors.append(f"{image.relative_to(ROOT)}: {error}")
        return
    if (width, height) != (1200, 630):
        errors.append(f"{image.relative_to(ROOT)}: expected 1200x630, got {width}x{height}")
    if image.stat().st_size > 2_200_000:
        errors.append(f"{image.relative_to(ROOT)}: social image exceeds 2.2 MB")
    for path, parser in pages.items():
        if not is_indexable(parser):
            continue
        if parser.meta.get("og:image:width") != str(width) or parser.meta.get(
            "og:image:height"
        ) != str(height):
            errors.append(f"{path.relative_to(ROOT)}: social image dimensions do not match metadata")


def main() -> int:
    errors: list[str] = []
    validate_public_tree(errors)
    pages = parse_pages(errors)
    if not pages:
        errors.append("no public HTML pages found")
    indexable_urls = validate_pages(pages, errors)
    validate_sitemap(indexable_urls, errors)
    validate_llms(indexable_urls, errors)
    validate_social_image(pages, errors)

    if errors:
        print("Pages site validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"Validated {len(pages)} HTML pages and {len(indexable_urls)} sitemap URLs.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
