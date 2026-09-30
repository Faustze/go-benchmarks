# go-benchmarks

Go performance measurements. Each one answers a single question with a number and includes the environment, the hypothesis written before the run, the methodology and the raw data, so it can be reproduced.

Site: https://go.faustze.tech/

## Measurements

| # | Question | Page |
|---|---|---|
| [001](articles/001-gc-pointers/) | What GC marking costs for a 10M-entry cache with and without pointers: from 1.2 ms to 1 s per timer-triggered collection | [go.faustze.tech/001-gc-pointers](https://go.faustze.tech/001-gc-pointers/) |

## Repository layout

```
go-benchmarks/
├── articles/              # articles: one folder per measurement + home page
│   ├── _index.ru.md       # home page text
│   └── NNN-topic/         # measurement: article, data, experiment code
├── cmd/site/              # server side: site build | serve command
├── internal/              # server side: content, render, shortcode, data, i18n, site
├── web/                   # client side: everything that reaches the browser
│   ├── layouts/           # page and shortcode templates (html/template)
│   ├── css/               # styles split into files, site.css bundles them via @import
│   ├── js/                # chart islands
│   ├── fonts/             # fonts (SIL OFL), served from the site
│   └── static/            # copied to the site root as is: CNAME, icons
├── i18n/                  # UI strings: ru.toml, en.toml
├── vendor/                # generator dependencies, the build never hits the network
└── .github/workflows/     # tests, build and deploy to GitHub Pages
```

The server side builds the site into `public/` (not tracked by git): Markdown becomes HTML, numbers and tables are filled in from `data.json` by shortcodes, styles are bundled into a single file. The client gets ready HTML, one CSS file and fonts. JavaScript is only needed for interactive charts, their data is embedded in the page.

## Measurement folder

```
articles/NNN-short-description/
├── index.ru.md  # article: TOML frontmatter between +++, text, {{< … >}} shortcodes
├── index.en.md  # translation, if any
├── README.md    # question → environment → hypothesis → methodology → results → conclusion
├── go.mod       # separate module if the experiment needs dependencies
├── main.go      # or *_test.go for a microbenchmark
├── run.sh       # reproducible run, all series one after another
├── report/      # log parsing and charts in Go
├── logs/        # raw data: gctrace, time -v, environment
├── data.json    # summary: the article takes numbers, tables and charts from it
└── img/         # images: shown instead of charts without JS
```

Binaries are built into `bin/` and are not tracked by git. Logs are named `*.log`. Only the article and `img/` from a measurement folder go to the site; code and logs are available here on GitHub.

## Building the site

```sh
go run ./cmd/site serve   # http://localhost:1313, rebuilds on changes
go run ./cmd/site build   # into public/
go test ./...
```

Generator dependencies live in `vendor/`. Run `go mod vendor` after `go get`.

## Rules

- The hypothesis is written in the README before the first run and is not edited afterwards.
- A single run is not a measurement: the number of runs and warm-up runs is recorded in the methodology.
- The mean is not used as the main metric, only the median, minimum and maximum.
- Numbers are compared only within the same environment.
