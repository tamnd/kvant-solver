# Reading the 2007 issues out of their own files

From 2007 the MCCME mirror carries a PDF of each issue, and those PDFs are born digital.
They have real fonts and a real text layer, so a page can be lifted out of the file with no model involved at all.
That is `kvant native`, and it is the cheapest lane in the project by a wide margin: a whole issue costs about fifteen seconds of poppler and nothing else.

The point of this document is that it is also the lane with the least evidence behind it, and what that costs.

## What it does

```sh
kvant fetch pdf --year 2010
kvant native --year 2010
```

`kvant fetch pdf` puts the issue file in the cache.
`kvant native` opens it, reads every page through `pdftotext -bbox-layout`, rebuilds the page in the same markup the vision lane produces, and writes the ones that come out whole.
It prints the sheets it would not read as a line that can be pasted:

```
kvant_2010_1: kvant ocr --issue kvant_2010_1 --sheets 4,7,8,10,11,...
```

Those go through the ordinary vision lane, and nothing about them is special afterwards.
A page's front matter says `extraction: native` or `extraction: vision`, and the rest of the pipeline does not care which.

## The map between the file and the scan

The file and the scan need not have the same number of surfaces in the same order.
An issue with a folded insert has a sheet the PDF does not, and everything downstream is keyed on the scan, so a page is filed by the number printed on it rather than by its position in the file.

The magazine leaves that number off more often than one would guess.
An article that opens under a full width title prints no folio at all, and from 2017 on there are thirteen or fourteen such pages an issue.
Those are placed by the pages either side of them: if the file has one page between printed 8 and printed 10, and the scan agrees that is one sheet, the page is page 9.
Where the arithmetic does not settle, the page keeps no number and goes to a model, because filing a page next to one it might not belong beside is worse than paying for it to be read.

A page placed this way still gets an empty `page_label`, because the label is what the paper prints and this page prints nothing.

## What it refuses, and why the yield is low

Every page is judged before it is written, and the standard is the one the vision pages are held to plus five measurements of its own.
Over 2007 to 2025 the lane read 2078 pages of 8052 and sent 5974 to a model, which is 26%.

| reason | pages | what it is |
| --- | --- | --- |
| stacked | 4169 | a formula set on more than one line |
| soup | 1661 | a banner or a run of algebra that arrived as loose single letters |
| unnumbered | 950 | mostly covers and inserts, which the scan carries and the file does not |
| math | 83 | more than 12% of the words are mathematics |
| everything else | 61 | the nine rules the vision lane's pages pass |

The two big ones are the same problem twice.
A text layer holds the characters of a fraction and not its rule, so a stacked formula arrives as its numerator and its denominator with nothing to say which was which.
A run of algebra with its operators set as separate glyphs arrives as `i S t r T t r o S r`.
Neither is worth telling apart, because both mean the page has mathematics the file cannot hand over and it goes to a model.

That is the honest yield of a cheap lane on a mathematics magazine, and it should not be tuned up.
Three quarters of these pages cost a model call either way.
The quarter that does not is a quarter of thirteen thousand pages, which is worth having for fifteen seconds an issue.

## Why there is a `check` subcommand

The nine rules are run over the native text as well, and they pass.
But they are being run on text the file produced, so what they establish is that the file is consistent with itself.
A broken font encoding, a column taken in the wrong order, a page filed as a sheet it is not: all three are self consistent and all three are wrong.

So the picture is the witness.

```sh
kvant native check --issue kvant_2010_1 --sample 100
```

This takes the pages the lane wrote, reads the scan of the same sheets through the vision lane into a corpus that is thrown away at the end, and compares the two word for word.
It writes `reports/native-check.md`, and `--keep DIR` leaves the second reading somewhere so the two can be put in a diff.

It runs where both records exist, which means an issue whose page images have been downloaded as well as its PDF.
Two of them have, and together they came to 24 pages of 12859 words.
The two readings put 13.8% of those words differently and 6.4% of them are words the file does not have in any spelling.
Nearly all of the gap between those two numbers is the model on the picture having misread a letter in a word the file has right, `кабитации` for `кавитации`, and the file is the one that is correct.

| issue | pages | words | differ | missing | accounted for | unread |
|---|---:|---:|---:|---:|---:|---:|
| kvant_2010_1 | 16 | 9447 | 13.3% | 5.7% | 87% | 7 |
| kvant_2017_6 | 8 | 3412 | 15.2% | 8.2% | 98% | 5 |

The twelve unread pages are all the vision lane failing its own rules on the scan, folio misreads and script violations, and none of them is evidence about the file.

## What the check found

Page 56 of the first issue of 2010 carries a half page advertisement for the МИФИ correspondence school, set as a JPEG.
The text layer has not one word of it.
The native reading of that page is three hundred and fifty clean words and it is missing a third of what is printed on the paper, and nothing on the page says so.
`pdftotext` on the page confirms it: the advertisement is simply not in the file.

This is the failure mode the lane cannot see, and it is the reason the check exists rather than being reasoned about.
Matter set as a picture does not arrive broken, it does not arrive.

It is not guarded against, and the second issue is the reason.
The obvious guard is a page whose images are large and whose text does not cover it.
On `kvant_2017_6` that guard cannot fire at all: every figure in the issue is drawn rather than pasted, `pdfimages -list` reports not one raster on any page of it, and matter set as a drawing is as absent from the text layer as matter set as a photograph.
So the guard would have caught an artifact of how the 2010 file was made and been blind to the general case, while on the 2010 issue it costs three good pages to catch the one bad one.
A rule that misses most of what it is for and charges three to one for the rest is not worth having.
The check is the guard.

## What the check gets wrong

The worst page of the second issue is a good page, and it is worth knowing why before anyone acts on the ranking.

Sheet 40 of `kvant_2017_6` scores 45.7% missing.
It is an article about counting operations, and it carries a coordinate grid and two rows of numbered cards.
Those are pictures in the file, so the lane writes `⟦figure⟧` where they stood, and the model looking at the photograph reads the numbers off and types them out.
Eighty one of the hundred and twelve words scored missing are those numbers, most of the remaining thirty one are the second reading's own misspellings, and not one word of the article's prose is gone.

Nothing here takes that out.
Dropping numbers from the comparison would hide a page that really did lose a table, and the whole point of the exercise is that the arithmetic is a way of choosing pages for a person to open, not a verdict.
So it is written down here and in the report the command generates, and the ranking stays honest about being a ranking.

## Where the archive has holes

`kvant fetch pdf` found no file for 25 issues: the fifteen the manifest lists for 2011, 2012 and 2013, and ten recent numbers across 2025 and 2026 that the mirror has not posted.
Those need another source before this lane can touch them, and until then their pages go to a model like the Soviet decades do.
