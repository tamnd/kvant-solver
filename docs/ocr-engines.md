# Which engine reads the pages

Four lanes were measured on the same nine sheets of 1975 №1, pages 40 to 48, which
are two column body pages with mathematics in most paragraphs and one figure. The
scans are what kvant.digital serves, 1200 by 1861, about 150 dpi.
The ChatGPT fleet row was measured later on six of those nine, because three of them never came back, and the section on the fleet says why.

| lane | seconds a page | words agreed | order agreed | mathematics |
| --- | --- | --- | --- | --- |
| Claude CLI, six at once | 7.5 effective, 35 to 45 each | reference | reference | yes |
| ChatGPT through the chatgpt-tool fleet | 162 a landed page | 91% | not measured | yes |
| GLM-OCR on a 4090, six at once | 1.36 | 81% | 83% | yes |
| Apple Vision on the laptop CPU | 0.4 to 1.2 | 93% | 80% | none |

Agreement is against the Claude transcriptions, which are the closest thing to ground truth we have until the sign-off pages are read by a person.
Words agreed is the share of reference words the lane also produced, order agreed is a sequence match over the same word lists.
The reference is one lane's own output, so the Claude row is not a score, it is the ruler.
What the table can say is how far the other lanes sit from it, and it cannot say which of the two is closer to the paper.

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

## The chatgpt-tool fleet, audited

chatgpt-tool drives real Chrome profiles with patchright and puts an OpenAI shaped endpoint in front of them, so ocr.Served can point at it the same way it points at vLLM.
It runs on server1, server2 and server3, which means the lane is a fleet of accounts rather than one browser, and the obvious question is whether that fleet is good enough to be the bulk lane instead of the 4090.

Nine sheets went to server2 and server3, three workers on each host, against the same Claude reference the rest of this page uses.
Six came back.
On those six the fleet agreed with the reference on 91.2% of its words and produced 16 words the reference does not have, out of 2773 reference words.
GLM-OCR on the same six agreed on 80.6% with 15 extra.
So the fleet is worth about eleven points of agreement over the 4090, which matches what the nine sheet bake off said, and it holds up when the pages are scored one at a time rather than in aggregate.

Speed is the problem.
server2 landed 2 pages in 9m31s and server3 landed 4 in 16m14s, so with both hosts busy the fleet turned out six pages in sixteen minutes, which is 162 seconds a landed page.
A single profile driven by hand was 148 seconds a page, so three workers on a host buy nothing.
The browser is the queue, not the network, and adding profiles to a host does not widen it.
At 162 seconds a page the 34000 page archive is 64 days on two hosts, against 13 hours on the 4090.

Three of the nine pages were lost, and neither loss is a bug we can fix.
One profile answered `chatgpt-profile-15 has no uploads left, the composer says 'wait 23 hours to upload again'`, which is the free account daily image cap.
Two more answered `chatgpt-profile-27 is signed out, somebody has to log this profile in again`, which needs a person at a browser.
A lane that loses a third of its pages to account state is a lane that needs a human on call, and that is the real ceiling, not the seconds.

One more thing came out of this that is worth writing down.
The first nine sheets all came back as `Please upload the image of the page you want transcribed.`
The proxy was flattening every request to its text parts before handing it to the browser, so the image was dropped and the model was answering a bare instruction with no page attached.
Nothing failed.
Nine HTTP 200s arrived with a fluent Russian sentence in each, and only the word count gave it away.
That is fixed in chatgpt-tool now, and the shape of it is the argument for the validate rules: a lane can be confidently wrong in a way that reads fine, so the pipeline has to check the answer and not the status code.

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

The fleet audit does not change the split, it just says which side chatgpt-tool belongs on.
It is a repair lane and a sign-off lane, three or four hundred pages of a run rather than thirty four thousand, and at that size 162 seconds a page and a profile that needs logging in every so often are both fine.
The Claude CLI is the other half of the same half, and it is the one to reach for when a repair batch has to finish tonight.
