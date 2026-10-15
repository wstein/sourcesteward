# Research decisions

- Hugo v0.168.0 is the current stable release (13 October 2026), verified against the
  [official release](https://github.com/gohugoio/hugo/releases/tag/v0.168.0) and
  [official news](https://gohugo.io/news/). Standard edition is sufficient: no Sass/images
  requiring extended features. Pin the executable and artifact hashes rather than latest.
- A local minimal template preserves current branding and avoids an external theme/module
  dependency. Hugo Goldmark renders Markdown with unsafe HTML disabled.
- Staging selected sources is preferable to mounting all docs/: explicit allowlists prevent
  publishing internal instructions and maintain one authored source per guide.
- Cobra metadata generation is preferable to scraping terminal help or manually duplicated
  flags; callbacks remain inert, so future read-only inspection cannot run during docs builds.
