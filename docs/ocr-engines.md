# Which engine reads the pages

Four lanes were measured on the same nine sheets of 1975 №1, pages 40 to 48, which
are two column body pages with mathematics in most paragraphs and one figure. The
scans are what kvant.digital serves, 1200 by 1861, about 150 dpi.

| lane | seconds a page | words agreed | order agreed | mathematics |
| --- | --- | --- | --- | --- |
| ChatGPT through the browser | 148 | reference | reference | yes |
| Claude CLI, six at once | 7.5 effective, 35 to 45 each | 97% | 97% | yes |
| GLM-OCR on a 4090, six at once | 1.36 | 81% | 83% | yes |
| Apple Vision on the laptop CPU | 0.4 to 1.2 | 93% | 80% | none |

Agreement is against the Claude transcriptions, which are the closest thing to
ground truth we have until the sign-off pages are read by a person. Words agreed
is the share of reference words the lane also produced, order agreed is a
sequence match over the same word lists.

## What the numbers mean

The 4090 lane is the only one that is both fast and produces mathematics. GLM-OCR
is 0.9B parameters, served by vLLM, and at six concurrent requests it reads a page
in 1.36 seconds, which puts the whole archive of about 34000 pages at 13 hours on
one machine. The browser lane puts the same archive at 58 days.

It is not as good. It agrees with the Claude lane on 81 words in 100 and it is
systematically shorter, 3038 words against 3240 over the nine sheets. On page 44
it dropped a 55 word run in the middle of a column, which is the failure that
matters: not a misread word, a paragraph that is simply not there. Nothing
downstream can see that, which is why the fast lane cannot be the only path.

Apple Vision is the fastest by a factor of three and is not a candidate. It reads
the two columns of a page as one, so its reading order is wrong on every body
page, and it returns no mathematics at all, which on this magazine is most of the
point.

## How GLM-OCR has to be driven

Two things came out of the runs that are not obvious.

The long prompt makes it worse. Sending prompt/ocr_page_ru.md as a system message
dropped agreement from 81% to 75% and made the run nine times slower, 12.5 seconds
a page against 1.36. It is a recognition model and not an instruction following
one, so it gets one short sentence and the structural markers are somebody else's
job.

Upscaling does not fit. At two times the page, 2400 by 3722, the run had produced
nothing after five minutes with the card at 19% and the server still holding the
requests, because the image tokens do not fit in the 16384 the server was started
with. There is more to find here and it needs a bigger context, not a bigger
image.

## The decision

The Engine interface is what this measurement bought. There is no single answer,
so the lane is a run time choice:

- GLM-OCR on the 4090 reads everything first. The validate rules reject what comes
  back short, unbalanced, out of language or without a folio line.
- Every rejected page and every page an article is assembled from goes to the model
  lane, which is chatgpt-tool or the Claude CLI behind the same wire.
- The sign-off pages are read by a person against the scan, and that number is what
  says whether the split above is set in the right place.

Both lanes speak POST /v1/chat/completions with an image in the message, so the
difference between them is a URL and a model name, which is what ocr.Served is.
