# Reading a decade

M3 read one issue. This is what changes when the same pipeline is pointed at twenty years, which is 239 issues and about 16000 pages, and it is mostly not the reading. One issue is an afternoon and fits in a terminal somebody is watching. Twenty years is days of machine time across restarts, tunnels that drop, a card that wedges and a laptop that sleeps, and the questions that matter become where is it up to, what did it cost, and what is still broken.

Four commands answer those, and they were written for this milestone.

```sh
kvant fetch pages --year 1975            # the scans, resumable, content addressed
kvant ocr --year 1975 --workers 6        # the fast lane, one card, six pages in flight
kvant repair --from 1970 --to 1989       # the pages the fast lane could not read
kvant coverage --from 1970 --to 1989     # what is finished and what is not
kvant report failures                    # every page that never read, with its class
kvant report cost                        # what the reading took, per year
```

## Two lanes, and why

The card reads a page in a second or two and gets most of them right. The measurements in `ocr-engines.md` put GLM-OCR on a 4090 at 1.36 seconds a page against 148 seconds for a browser session, which is the difference between a fortnight and a year, so there is no version of this where the whole run goes through a model that reads instructions.

It is also not good enough on its own. Over the first 193 attempts of the 1975 run, 116 pages were accepted and 77 attempts were thrown away, so the first pass keeps about three pages in five. What throws them away is not evenly spread:

| rule | attempts | what it is |
| --- | ---: | --- |
| script | 37 | two alphabets welded into one word, `Одеcca` with a Latin c |
| folio | 22 | the printed number disagreed with the sheet |
| math | 5 | unbalanced delimiters |
| short | 3 | a truncated answer |
| language | 3 | the page came back translated |
| latex | 2 | a span KaTeX will not parse |

The script rule is the one that matters, and it is the reason the rule exists. A page where `химico` has been substituted for `химико` reads as text, passes every check that looks for a refusal or a short answer, and is wrong in a way that nothing downstream can see. The card produces those at temperature 0 on a real fraction of pages, and asking it again produces the same page, because the sampling is pinned and the answer is deterministic.

So the tail goes somewhere else. `kvant repair` takes every dead page out of the queue and reads it with a model that follows instructions, at roughly a hundred times the cost of a page and on the one page in three that needs it. The work list is the queue rather than a person: nobody copies sheet numbers out of a report.

```sh
kvant repair --from 1970 --to 1989 --rule script,language
kvant repair --from 1970 --to 1989 --dry-run
```

## The signature mark

One failure class was a bug in this repo and it is worth writing down, because it would have cost thousands of pages.

Sheet 21 of 1975 №2 and sheet 21 of 1975 №3 both failed the folio rule with the same complaint: the page prints 2, the sheet was expected to print 19. Both are printed page 19, and both really do print 19, at the bottom right corner. They also print `2*` at the bottom left, which is the printer's signature mark, the number of the gathering. The band the folio reader crops is the full width of the page, so it holds both numbers, and the parser took the first plausible one.

Every issue of the run is set the same way. That is one wrong page per gathering per issue, in a class the folio rule then rejects, which over twenty years is thousands of pages sent to an expensive lane to be read again for a number that was never the page number. `ParseFolio` now drops a number with an asterisk after it before it looks for the folio.

## Where the state lives

Nothing about a decade run is held in memory, because nothing that runs for days can be.

The **cache** holds the scans, addressed by content hash, so a sheet already downloaded is never asked for again and a fetch that broke on issue nine resumes at issue nine.

The **queue** holds one job per page, with its attempts and the reason for each. A page that failed three times is dead rather than pending, so a run ends instead of retrying forever, and `kvant repair` is what picks the dead ones up later.

The **corpus** holds the pages themselves and is the thing that has to be complete. `kvant ocr` skips a sheet that already has a page file whatever the queue says, so a queue thrown away costs nothing and a corpus thrown away is the run happening again, which is right.

The **ledger** is new here. It is a JSON line per attempt under `cache/ledger/ocr.jsonl`, holding the page, the engine, the seconds and the tokens, appended and flushed as the run goes. A summary printed at the end of a run is gone when the terminal closes, and the milestone asks what a year cost. Rejected attempts are lines too: a page read three times and thrown away three times cost three times what a page that worked cost, and a cost report that counted only the corpus would understate the run by exactly the amount worth knowing.

## Reading the cost table

```
year        pages   attempts        time         input        output      a call
1975          116        193      46.9 m        536850        200753      14.6 s
```

Two columns need care. The time is the sum over the workers and not the clock on the wall, and a call is the average latency of one call, not the throughput. Six workers against one card give 14.6 seconds a call and about 2.4 seconds a page, because the card batches them. Divide by the workers to plan a run.

Tokens are only there for a lane that reports them. A local program prints Markdown and exits, and the report says how many attempts came with no numbers rather than recording them as free. Money is off unless somebody passes `--price-in` and `--price-out`, because three of the four lanes bill nothing per token and the fourth changes its rates, so a table of prices in the source would be authoritative and wrong.
