# kvant-solver

Go toolchain that turns the *Квант* archive into a tagged Markdown corpus, translates it, and solves its problems.

The corpus it produces lives in `tamnd/kvant`. This repo is code.

## What it does

*Квант* is the popular physics and mathematics monthly of the Russian Academy of Sciences, published since January 1970, ISSN 0130-2221. The archive runs to 516 issues and roughly 34000 printed pages.

```
kvant.digital ──┐
kvant.mccme.ru ─┼─ sync ─→ manifests + queue
mathnet.ru ─────┘             │
                              ▼
       ┌── native     pdftotext, the 2007+ issue PDFs are born digital
fetch ─┼── publisher  the text the archive already carries, about a quarter of it
       └── vision     page JPEG through the OCR fleet, about 21000 pages
                              │
                    page Markdown, which is the ground truth
                              │
             assemble ─→ articles ─→ tag ─→ audit ─→ translate ─→ solve
```

The scope is the whole magazine and not only the problem section, so the pipeline transcribes every printed page and assembles articles out of pages rather than scraping articles directly. That is the only way the covers, the answer columns, the chess page and the unattributed fillers survive.

## Install

```sh
go install github.com/tamnd/kvant-solver/cmd/kvant@latest
```

Needs poppler for `pdfinfo`, `pdftotext`, `pdftoppm` and `pdffonts`:

```sh
brew install poppler
```

## Use

```sh
export KVANT_CORPUS=$HOME/github/tamnd/kvant

kvant sources probe
kvant issues sync
kvant fetch pages --issue kvant_1975_1
kvant fetch pdf --year 2007
kvant textguard --year 1975
kvant native --year 2010
kvant native check --issue kvant_2010_1
kvant ocr --year 1975 --workers 6
kvant repair --from 1970 --to 1989
kvant assemble --issue kvant_1975_1
kvant audit
kvant coverage --from 1970 --to 1989
kvant report failures
kvant report cost
```

`kvant --help` lists the rest, `docs/decade-run.md` is how the last five of those fit together over twenty years of issues, and `docs/native-lane.md` is the two `kvant native` lines and why the second one exists.

The scans are tens of gigabytes of JPEG and they do not belong in a repo of text, so they land in a cache outside the checkout: `--cache`, or `KVANT_CACHE`, or the user cache directory. A sheet whose bytes are already there is not asked for again, so a run that broke on issue nine picks up where it stopped.

## The site

`kvant publish` turns the corpus into a static site in three languages, and `kvant serve` shows it the way the hosts will before anything is uploaded:

```
go build -o kvant ./cmd/kvant
./kvant publish --corpus ../kvant --out site     # about two minutes on a laptop
./kvant serve --dir site                         # http://localhost:8080
```

Everything on it links relatively and pages have no extension, so one build works at the root of a domain and under a path, and on any host that serves `x.html` at `x`. Cloudflare Pages and GitHub Pages both do.

| flag | |
|---|---|
| `--lang ru,en` | publish only these languages; the default is every one the corpus has |
| `--site-url` | where this copy will live, used by the missing page and the sitemap |
| `--canonical-url` | the address search engines should prefer, when this copy is a mirror |
| `--no-sheets` | leave out the page by page sheets, about half the size of the site |
| `--max-files`, `--max-mb` | fail the build when it outgrows a host, before anything is uploaded |
| `--strict` | fail on a formula that could not be typeset rather than marking it |
| `--report file` | write those formulas out, file and line |
| `--clean=false` | keep what is already in `--out` |

The build refuses to write anything a static site of text is not made of: no images, no PDFs, no script but its own two, and no tag that fetches something from somewhere else. It then opens every link it wrote and fails on one that goes nowhere.

The corpus repo deploys it on every push to `main` (`.github/workflows/site.yml` there): the whole site to Cloudflare Pages at kvant.tamnd.com, and a mirror without the sheets to GitHub Pages at tamnd.github.io/kvant, with its canonical links on the first.

## Layout

```
cmd/kvant     the CLI
corpus        front matter, identifiers, the page and article model, hashing
source        clients for kvant.digital, kvant.mccme.ru and Math-Net.Ru
fetch         scan and PDF download, content addressed, resumable
pdfsrc        poppler wrappers
textguard     decides which extraction path a page takes
native        the 2007+ issue PDFs read out of their own text layer
ocr           page prompts, batching, retry, failure classification, the cost ledger
fleet         SSH, tunnels, lease based work distribution
queue         durable on disk queue
coverage      what is finished and what is not, per issue and per year
report        the failure list and what the reading cost
assemble      page blocks into articles
problems      the M and F problem set, condition paired with published solution
tags          permanent tags, assignment and verification
translate     ru into en, vi, zh, ja
solve         reference blind solver and verifier
grade         our solution against the one the magazine printed
pricing       what a run cost, per route and per model
audit         corpus checks
publish       the static site, built from committed Markdown
```

## Politeness

The archive is hosted by a mathematics education charity. The crawler runs one request at a time per host with a real delay, honours `robots.txt` including `Clean-param`, and sends an honest user agent. A transfer that broke halfway is asked for again after a growing wait, because a fifty year sweep meets a reset that means nothing at all. A 403, a 429 or a 404 is never asked again: those are answers and not accidents. If a stage needs to run faster the answer is to ask, not to add workers.

## Licence

MIT for the code. The corpus it builds is derived from copyrighted material, see the licence in the corpus repo.
