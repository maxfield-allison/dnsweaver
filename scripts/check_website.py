"""Check rendered product pages and their local destinations."""

from __future__ import annotations

from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parent.parent / "build" / "website"
DOMAIN = "dnsweaver.com"


class Page(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.links: list[str] = []
        self.ids: set[str] = set()
        self.canonical = ""
        self.description = ""
        self.headings = 0

    def handle_starttag(
        self, tag: str, attributes: list[tuple[str, str | None]]
    ) -> None:
        attrs = dict(attributes)
        if attrs.get("id"):
            identifier = str(attrs["id"])
            if identifier in self.ids:
                raise ValueError(f"duplicate element id: {identifier}")
            self.ids.add(identifier)
        if tag == "h1":
            self.headings += 1
        if tag == "link" and attrs.get("rel") == "canonical":
            self.canonical = str(attrs.get("href", ""))
        if tag == "meta" and attrs.get("name") == "description":
            self.description = str(attrs.get("content", ""))
        for key in ("href", "src"):
            if attrs.get(key):
                self.links.append(str(attrs[key]))


def main() -> None:
    pages: dict[Path, Page] = {}
    for path in sorted(ROOT.rglob("*.html")):
        parsed = Page()
        contents = path.read_text()
        parsed.feed(contents)
        if parsed.headings != 1 or not parsed.description:
            raise ValueError(f"heading/description missing: {path.relative_to(ROOT)}")
        relative = path.relative_to(ROOT).as_posix()
        route = "/" + relative.removesuffix("index.html")
        if parsed.canonical != f"https://{DOMAIN}{route}":
            raise ValueError(f"unexpected canonical: {relative}")
        if "data-preview" in contents or "private preview" in contents.lower():
            raise ValueError(f"preview controls escaped: {relative}")
        pages[path] = parsed
    for path, parsed in pages.items():
        for link in parsed.links:
            url = urlsplit(link)
            if url.scheme and url.scheme not in ("http", "https"):
                raise ValueError(f"unexpected link scheme: {url.scheme}")
            if url.netloc and url.netloc != DOMAIN:
                continue
            decoded = unquote(url.path)
            target = (
                ROOT / decoded.lstrip("/")
                if decoded.startswith("/")
                else path.parent / decoded
            )
            target = target.resolve()
            if not target.is_relative_to(ROOT.resolve()):
                raise ValueError("local link escapes website")
            if target.is_dir():
                target /= "index.html"
            if not target.is_file():
                raise ValueError(
                    f"missing local destination: {link} from {path.relative_to(ROOT)}"
                )
            if (
                url.fragment
                and target in pages
                and url.fragment not in pages[target].ids
            ):
                raise ValueError(f"missing anchor: {link}")
    if len(pages) != 4:
        raise ValueError("expected home, pricing, privacy and404 pages")
    print(
        f"Checked {len(pages)} rendered documents, canonical URLs, assets, links and anchors"
    )


if __name__ == "__main__":
    main()
