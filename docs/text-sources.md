# Where the text comes from

Every page in this corpus is a reconstruction of a printed page.
A model looked at a photograph of it, or poppler pulled it out of a PDF's text layer, and both of those are wrong in ways that only a second reading can find.

So the first question for any new source is not how good it is.
It is whether it was made independently of the ones already here.
A source that is merely a copy of one we have adds nothing at all, however clean it looks, and two of the six below turned out to be exactly that.

This document is what each source is, what it is worth, and what was looked at and rejected, so that nobody goes looking a second time.

## The six

| source | what it holds | coverage | what it is used for |
|---|---|---|---|
| kvant.digital | publisher supplied HTML with formula spans and figures | 2281 of 11422 contents items | article bodies, `extraction: publisher` |
| archive.org | ABBYY OCR of the bound year volumes | 1970 to 1992, 23 volumes | the second witness, `kvant vision check` |
| kvant.mccme.ru | the PDF archive, per issue and per article | every year | every scan and every text layer this project reads |
| mathnet.ru | article metadata | partial | M7, references and not text |
| kvant.ras.ru | a mirror of mccme | every year | nothing, see below |
| elementy.ru | editorial announcements and links | 69 issues, 2009 to 2021 | nothing, see below |

## kvant.digital

The publisher's own site, and the only place on the internet where somebody has typed this magazine out rather than photographed it.
The markup carries formula spans and marks where the figures stand, which is more structure than any reading of ours will ever recover.

It covers 2281 of the 11422 contents items, which is 20% of the run, and where the contents gives page numbers it is 1675 page slots.
That is 13% of the Soviet decades and 7% of 1990 to 2006.
It will not replace the vision lane and it was never going to.
It is 2281 articles at publisher quality for no model calls at all.

Where it has the text, the article body comes from it and says `extraction: publisher`.
The pages underneath are left exactly as they are, because pages are ground truth and an article is a view of them, and a page that was never read does not become read because somebody typed the article that runs across it.

The typed text is checked rather than trusted.
It goes through `ocr.Validate` like everything else and is compared against the body assembled from our own pages, so a piece of markup that turns out to be half an article shows up instead of quietly looking better than the reading it replaced.

One thing it does not carry is the title.
What the publisher types out is the prose, the title being a field of their page rather than the first line of it, so `publisher.Titled` puts it back.
Without that, the articles built from typed text would be the only ones in the corpus whose body does not name them.

## archive.org

One item, `kvant-journal`, holding 1970 to 1992 as twenty three bound year volumes, each scanned and run through ABBYY.
It is a second reading of exactly the years this project reads with a model, made by somebody else, from a different set of paper, with a different engine, and it is free.

It is not a better reading and it is not written into the corpus.
The paper is Soviet newsprint and the OCR shows it: Latin letters land in Cyrillic words, ё is lost, and mathematics comes out as punctuation.
Roughly three quarters of the words our own reading finds on a page are in the archive's text of the same page, and a page that is entirely correct still fails to contain a third of what ABBYY produced.

What it is good for is disagreement, which is the one thing the vision lane has never had.
`kvant vision check` is that, and it costs three files per year and no model calls.

### Two things about the format

The per leaf offsets in `_hocr_pageindex.json.gz` count **characters, not bytes**.
Every page of this magazine is Cyrillic, so in UTF-8 the byte length is nearly twice the character length, and slicing the search text by those numbers as if they were bytes lands halfway through the volume and returns real Russian prose from the wrong page.
It reads as an alignment that is merely poor rather than as a bug.
`archiveorg.Build` refuses an index that does not cover the text in characters, and there is a test with Cyrillic in it for exactly that reason.

The alignment itself is simpler than it looks.
The two scans are the same scan: somebody bound twelve issues into a year volume and photographed every surface in order, covers and inserts included, which is what kvant.digital holds issue by issue.
The nth leaf of the volume is the nth sheet of the year and the whole alignment is a running total of sheet counts.
That is a claim about somebody else's scanning rather than a fact about the format, so it is checked against the sheet total, and it holds for every year but 1990, which is seven leaves short and is refused rather than guessed at.

The printed page numbers are deliberately not used to do the alignment, only to check it afterwards.
They are OCR of a number in the corner of newsprint, missing on every cover, and an alignment built on them would fail worst exactly where the scan is worst, which is where it is needed most.

## kvant.mccme.ru

The PDF archive, and the spine of this project.
Every scan the vision lane reads and every text layer the native lane lifts comes from here.

It is PDF from top to bottom and it is not a text source in the sense this document means.
From 2007 the files are born digital and carry a real text layer, which is what `kvant native` is for, but that layer is a property of the file rather than a separate reading of the paper, so it is not a witness against itself.

The per article PDFs, `kv0103akulich.pdf` sitting next to `29.pdf`, are a lead on article boundaries and not on text.

Fifteen issues across 2011 to 2013 and ten across 2025 to 2026 have no `pdf_url` at all and still need a source.

## mathnet.ru

Article metadata: authors, titles, page ranges, and identifiers.
That is M7's business and it is not text.
It is listed here so that nobody mistakes a complete metadata record for a complete article.

## kvant.ras.ru, rejected

This looked like the most promising lead of the six and it is the least useful.
It is a mirror of the mccme archive rather than a new full text site, so it is the same PDFs behind a different hostname.
A copy of a source we already have is worth nothing as a witness no matter how it is served.

Its certificate expired in October 2025 and nobody has renewed it.

## elementy.ru, rejected

The estimate going in was about fifty reprinted articles, 2009 to 2021, which would have been the only place two independent typings of the same article could be compared.
It is not that.

All 69 Kvant issue pages were crawled.
Each one carries an editorial announcement of the issue and a short selected section, 804 to 1943 Russian words, 66708 across the whole set.
That prose is somebody describing the issue, not a reprint of anything in it.

Then the links.
145 PDF links across the 69 pages, and 138 of them point at `kvant.mccme.ru`, which is to say at the files this project already downloads.
The remaining seven are «Квант для младших школьников» sections for 2012 and 2013, hosted on elementy itself, and they are PDFs too.

So elementy.ru is an index and a signpost back to mccme.
There is no article HTML on it, there is no second typing of anything, and there is nothing here to compare against.
The seven младших школьников files are the only artefact on the site that mccme may not have, and they are worth a look if that section ever matters, but they are not a text source.

## What this means

There is no second site serving the article text of this magazine as HTML.
kvant.digital is the only typing that exists and it covers a fifth of the run.

So the three witness comparison this milestone hoped for does not exist for any article, and the corpus has two witnesses where it has any at all: our reading against archive.org for 1970 to 1992, and our reading against kvant.digital's typing wherever that typing exists.
Everything from 1993 on has one witness and nothing to check it against, which is worth remembering before anybody quotes a quality number for those years.
