# Translation

Квант ran for over fifty years in Russian. Almost nobody who would get something out of it can read it. That is the whole reason this half of the project exists, and it is why the target is the entire magazine rather than the problems: the articles are where the magazine actually taught anything, and they are the part that has never been available anywhere else.

English and Vietnamese come first, Chinese and Japanese behind them.

## The part that is hard

Translating twenty thousand pages is a question of time and tokens. Keeping them translated is the actual problem.

A page gets reread because the OCR improved. A glossary term gets fixed. The register instruction gets edited. Any one of those means some subset of the translated corpus no longer corresponds to what it claims to be a translation of, and there is no way to tell which subset by looking at the files. Rereading them all through a model to find out costs as much as the translation did, every time.

So the answer has to be readable off the files themselves, without a model call. That is what the hashes in `corpus.Translated` are for, and it is why they are written on every file even though nothing reads them at translation time.

## The four tests

```yaml
translated_from: content/ru/1975/08/pages/0012.md
source_content_sha256: 7f3a...
translation_model: gpt-5
translation_run: 1975-en
glossary_version: 12
glossary_terms_sha256: c41d...
prompt_sha256: 9b02...
```

`translate.Check` reads those against the corpus as it stands now and returns four booleans.

**The source moved.** `source_content_sha256` against the hash the Russian file carries today. This one is stale on its own: whatever else is true, this is a translation of a document that does not exist any more.

**The instruction moved.** `prompt_sha256` against the hash of the register instruction as it is written today. Also stale on its own, and deliberately so. The register is the thing the translation is being held to, so changing it means every file was held to something else. A file written before the hash was ever recorded is left alone rather than treated as a mismatch, because otherwise the first edit would send every page from the early runs back through for nothing.

**The glossary version moved.** `glossary_version` against the current version. This one is *not* stale on its own. It is a screen, and it is cheap: if the version has not moved then no row can have moved and there is nothing further to compute.

**The rows this file was shown moved.** `glossary_terms_sha256` against the hash of the rows the file would be shown today. This is the one that decides.

```
stale = sourceMoved || promptMoved || (glossaryMoved && termsMoved)
```

The last conjunction is what makes the pipeline usable at all. Without it, adding one word about optics bumps the version, every translated file in the corpus goes stale, and the archive has to be retranslated to record a term that appears in four articles. With it, adding that term leaves the number theory alone.

The rows are recomputed against the Russian as it stands now rather than as it stood then, so a term that was in the glossary all along but only appears after the page was reread counts as a change to that file. That is the right answer: the file would be translated differently today.

Each language has its own column and its own hash, so fixing the Vietnamese does not send the English back through.

## The glossary

`manifests/glossary.yaml`, in the corpus repo, keyed on Russian because Russian is the source.

```yaml
version: 12
terms:
  - ru: производная
    en: derivative
    vi: đạo hàm
    note: not differential coefficient
    quantum: derivative
```

It exists because a corpus this size cannot be translated in one call, so it is translated in thousands of them and nothing carries context from one to the next. Without it «производная» comes back as derivative in one article and as differential coefficient in the next, both defensible, and the archive reads as though it had forty translators who never met.

The version moves when a term changes and not when the file is rewritten. `SameTerms` compares the content of the rows rather than the slice, so a reordering is not a change. Given what a bump costs downstream, that distinction is worth the code.

`quantum` is the rendering the English *Quantum* used between 1990 and 2001 where a mapping row exists. It is a check on our terminology, not a source. Recording it means a disagreement with the published English is visible and deliberate rather than silent.

### Finding terms in inflected Russian

This is the part that decides whether the glossary works at all.

The glossary is written in the nominative and Russian articles almost never use it. «производная» turns up as «производной», «производную», «производных». A matcher that only fires on the dictionary form is a glossary that is quietly not being applied, and nothing about the output would look wrong.

So matching is on stems. `glossary/mention.go` strips the noun and adjective endings that carry the cases a technical term actually appears in, and refuses to cut a word below four characters, because below that the endings start eating short words whole and «ток» collides with «то».

That floor leaves a gap: «ток» and «тока» both survive intact and never meet. So there is a second pass, where two stems match if one is the other with a tail of at most three ending letters on it. «током» and «токах» match «ток»; «токарь» does not, because р is not a letter Russian endings are made of.

Multi word terms match when all their words are present, without checking adjacency. Over inclusion is the safe direction here. A row the translator did not need costs a line of prompt. A row it did need and was not shown costs a wrong term in the archive that nobody will ever notice.

Formulas are cut out before any of this. «ток» is three letters and the inside of a LaTeX span is full of three letter fragments, so matching there produces rows nobody needs and makes a file stale for a term it never used in prose.

## Chunking

A body is cut on blank lines, which is a boundary the Markdown already has, so joining the answers with a blank line between them reproduces the block structure exactly. No sentence is ever cut in half and nothing has to be reassembled.

Two budgets, not one:

- `ChunkChars`, 6000
- `ChunkSpans`, 15

Characters alone are the wrong budget. What a model cannot do is copy forty formulas character for character while translating the sentences between them, and a chunk fails on the mathematics long before it fails on length. A page of worked algebra is short prose and dense notation, which is exactly the shape that slips past a character budget and comes back mangled.

A block that is over budget on its own gets a chunk to itself rather than dragging a neighbour with it. Splitting inside a block would cut a sentence or a display.

What chunking costs is context: whoever translates chunk four has not read chunk three. The glossary is what makes up for that, and it is why the glossary is built before anything is translated rather than after.

## Checking the mathematics

`translate.Verify` compares the math spans of the source against the math spans of the translation, in order, character for character. A mismatch sends the chunk back, up to `--retries` times, and what survives that is reported rather than written out quietly.

It only checks the mathematics, on purpose. Whether the prose reads well is a judgement and needs a reader. Whether the formulas came back the way they went in is a fact, it is the failure that actually happens, and it is the one nobody catches in a language they do not read. A page whose algebra was silently retyped is worse than a page that was never translated, because it looks finished.

It catches a dropped span, an invented one, a rewritten one, a display collapsed to an inline, and a delimiter left open.

## Register

Квант was written by working mathematicians and physicists for fifteen year olds, and the voice is specific: direct, unhurried, completely unafraid of the reader. It does not talk down and it does not perform.

A translation that turns it into a Springer monograph has failed even when every term in it is right, and so has one that turns it into a popular science article. This is the failure mode that no hash catches, which is why the exit condition for this milestone is a native reader signing off a sampled issue in each language rather than a number.

## Running it

```
kvant translate --lang en --year 1975 --write
```

Every file is checked before it is sent, so a second run costs nothing on the files that are still current. `--force` overrides that.

```
kvant translate --lang en --audit
```

Reads the whole tree and reports what is stale and why, and what has never been translated at all. Those two are counted separately: one needs translating, the other needs translating again, and folding them together hides how much of the corpus was never done.

With `--write` the audit also lands in `reports/translation-audit.md`.

## What is not done

`manifests/quantum-map.yaml`, the 1990 to 2001 article mapping, needs the *Quantum* tables of contents and has not been built.

The sampled register audit and the full year into each language need the fleet, which is saturated with the OCR of the Soviet decades. The machinery is here and tested; no year has been run through it yet.
