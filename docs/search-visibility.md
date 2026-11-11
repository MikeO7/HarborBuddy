# Search and AI visibility

The project site at <https://mikeo7.github.io/HarborBuddy/> targets people who search for a way to auto-update Docker containers, and people leaving Watchtower, which was archived on 2025-12-17. It is written so that both search engines and AI answer engines (ChatGPT, Claude, Perplexity, Google AI Overviews) can quote it accurately.

## Editing the site

Page sources live in `site/pages/`. Each file starts with a JSON header in an HTML comment (title, description, updated date, FAQ entries), followed by the page body. Build and check:

```bash
python3 scripts/build-site.py
python3 scripts/validate-pages-site.py
```

The build writes `docs/site/**/index.html`, `404.html`, `sitemap.xml`, `robots.txt`, and `llms-full.txt`. `styles.css`, `llms.txt`, `assets/`, and the IndexNow key file (a 32-character hex name ending in `.txt`, public by design) in `docs/site/` are edited by hand. CI fails when the generated files are out of date. The editable source of the social image, `site/assets/harborbuddy-social.svg`, is not published; export it to `docs/site/assets/harborbuddy-social.png` after changing it.

## Strategy

- **Answer first.** Each page says what HarborBuddy is, or how it compares, in its first paragraph. Answer engines quote these passages.
- **Search intent.**
  - `/compare/watchtower/` targets "watchtower alternative", "watchtower archived", and "migrate from watchtower".
  - `/compare/` targets "docker container update tools".
  - The other comparison pages and the docs pages target long-tail tasks such as "docker compose auto update", "docker update rollback", and "clean up old docker images".
- **Honest comparisons.** Every comparison table is dated and links to each project's own documentation. It keeps HarborBuddy's gaps visible: no notifications, no UI, no private registry login, and a noncommercial license. Before you change a claim about another project, check it against that project's documentation and update `updated` and the "Last reviewed" line.
- **Structured data matches the visible page.** FAQ entries come from one `faq` list in each page header. The build renders that list as both the visible `<details>` and the FAQPage JSON-LD. Every page also has WebSite and WebPage (or TechArticle) data. The home page has SoftwareApplication, and other pages have a BreadcrumbList.
- **AI crawlers are welcome.**
  - `robots.txt` names the OpenAI, Anthropic, Perplexity, Google-Extended, Applebot-Extended, Bing, and Common Crawl agents.
  - `llms.txt` holds a short summary, key facts, and linked pages.
  - `llms-full.txt` holds the full text of every page.
- **Fresh index.** After each deployment, the Pages workflow submits every sitemap URL to IndexNow, which covers Bing, Yandex, and others.

## Steps that need account access

A deployment does not do these. Crawlers read `robots.txt` only at the domain root (`mikeo7.github.io/robots.txt`), so the copy under `/HarborBuddy/` is advisory, and search engines will not discover the sitemap from it. Submit the sitemap by hand:

1. Verify the site in [Google Search Console](https://search.google.com/search-console) with a URL-prefix property for `https://mikeo7.github.io/HarborBuddy/`. Use the HTML-tag method by adding a `google-site-verification` meta tag to `head()` in `scripts/build-site.py`. Then submit `sitemap.xml`.
2. Optionally verify the site in [Bing Webmaster Tools](https://www.bing.com/webmasters) to see IndexNow and crawl reports.
3. Consider a custom domain. A project site on `github.io` shares its domain with many others, and a dedicated domain builds its own authority over time.
4. Post the release where self-hosters look, such as r/selfhosted, r/homelab, and awesome-selfhosted. Links from those places matter more than anything on the page.

No ranking or AI citation is guaranteed.
