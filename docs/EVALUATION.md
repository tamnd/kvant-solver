# Measuring the solver

Задачник «Кванта» set a problem in one issue and printed its solution two to four issues later, under a heading that named the numbers it was answering. That is what makes this archive worth solving rather than just reading. Every problem that came through the reading lane with both halves intact is a question with a dated, human authored answer attached, written by editors who were not thinking about benchmarks, and there are thousands of them.

That also makes it very easy to measure nothing at all.

## The one failure that looks like success

If the published solution reaches the solver, everything downstream still works. The solver writes a correct solution, both judges pass it, the grader marks it correct, and the scorecard reports a high score. Nothing errors, nothing looks odd, and the number measures how well a model can paraphrase a paragraph it was just shown.

There is no test that catches this after the fact, because a leaked run and a clean run produce the same shape of output. So it is prevented structurally instead.

`solve.Problem` is what the solver is handed, and it has three fields: the identifier, the subject and the statement. There is nowhere to put a published solution. Not "there is a field we are careful not to set", but no field, so no call site can pass one and no later edit can start doing it quietly. The printed answer is a separate argument to `Grade`, which runs after `Solve` has returned.

The prompt interfaces are split the same way. `Prompts` has the six calls `Solve` makes and none of them takes a published solution. `Grade` lives on a separate `Grader` interface that `Solve` never holds. An implementation of `Prompts` cannot see the printed answer even if whoever wrote it wanted to.

Two tests hold this in place. `TestThePublishedSolutionNeverReachesTheSolver` puts a distinctive token in the printed solution, runs the whole pipeline through a fake client that records every request, and asserts the token appears on zero of the solving calls and on exactly one grading call, which is the first call made after the solution was fixed. `TestSolveHasNowhereToPutTheAnswer` reflects over `solve.Problem` and fails if a field was added, with the reason in the failure message.

## The pipeline

```
reference ─┐
           ├─→ select ─→ truth judge ─┐
candidate ─┤              audit judge ─┼─→ both pass? ─→ done
   × 3     ┘              dimension   ─┘        │
                                                └─ no ─→ correct ─→ (bounded loop)
```

A reference solution is worked first, on its own, and is never published. Three candidates are then worked independently, each pushed down a different route, because three attempts along the same road are one attempt with more tokens spent. The selector sees the reference and the candidates and picks one, and it is told to work out which answer is right rather than count votes, since they may all be wrong.

Two judges then read the selected solution and both have to pass it.

They are not redundant. The truth judge is shown the reference and asked which of the two is right, which makes it prone to accepting whatever two texts agree on. The audit judge never sees the reference, is told to assume there is a fault, and is given a list of the ways these problems usually break: a step that does not follow, a degenerate case the argument excludes, equality in an inequality, a divisor that could be zero, generality claimed from an example. A solution that talks its way past one still has to survive the other.

A judgement that cannot be parsed is a failure, not a pass. Defaulting the other way would let every malformed reply through as verified, which is again the failure mode that looks like success.

Physics gets a third check that is not a model. `solve.Dimensions` flags a solution whose working is in metres and seconds and whose answer is a bare number, because the units went missing somewhere between the last line of algebra and the result and a prose judge reads straight past it. It is deliberately one narrow rule. A solution that never mentions a unit is left alone, since plenty of the physics in this magazine asks for a ratio, an angle or a proof, and demanding units of those would send the correction loop after solutions with nothing wrong with them. Worse, it would invite a model to bolt units onto a dimensionless answer to satisfy the checker.

## Grading

`kvant solve` marks each finished solution against the printed one and returns one of four gradings.

`CORRECT` and `INCORRECT` are what they sound like. `PARTIAL` counts as a miss in the score, because the magazine printed one answer and either the solution reached it or it did not. `UNGRADED` is the interesting one.

A problem is ungraded when the magazine never printed a solution to it, or when the solution it printed came off the scan too damaged to mark against. Those are left out of both halves of the score rather than counted as misses. A page this corpus failed to read is not a problem the solver got wrong, and counting it as one would make every improvement to the reading lane look like an improvement to the solver, which is the opposite of what the scorecard is for. The count of ungraded problems is printed alongside the score so that nobody has to guess how much of the set actually ran.

The grader is told to mark the answer and not the method. The printed solution is usually the shortest one the editors could find and is often not the only one, so a different valid route to the same answer is correct.

## What the false pass rate is for

The judges are the only check the solver has, and they cannot audit themselves. A run where both judges approve everything and the magazine disagrees with a third of it is a run whose judges are agreeing with the solver rather than checking it, and nothing inside the pipeline can detect that.

The printed solutions can. `solve.Compare` sorts every problem into four boxes by whether the judges verified it and whether the magazine marks it correct, and the false pass rate is the share of verified solutions the magazine says are wrong. It is the only number in the run that measures the verification rather than the solving, and it is the reason the grading half exists at all rather than just a score.

## The sets

A set is a preset with a name, not a pair of numbers on the command line, so that "the smoke set" means the same thing in a commit message, in a scorecard and six months later.

`smoke` takes two problems per decade and subject. It answers whether the pipeline runs, not whether it is any good. `decade-stratified` takes twenty five per decade and subject and is what the scorecard is computed from.

Both are stratified by decade and subject and drawn evenly across the numbering rather than randomly. There is no seed to record, so a set can be rebuilt from the manifest at any time and a scorecard nobody can reproduce is an anecdote. Weighting by decade rather than by how much of each decade happens to be read matters, because the corpus is not read evenly and a set drawn from raw availability would silently become a set about whichever years finished first.

Only problems with both halves are eligible. A problem with no printed answer cannot be scored, and letting one in would mean the score is computed over whichever members happened to have ground truth.

## Running it

```sh
kvant problems build --write          # pair the two halves, write manifests/problems.yaml
kvant eval --set smoke --write        # draw the set
kvant solve --set smoke --mode fast   # check the pipeline runs
kvant solve --set decade-stratified --write
```

`--mode fast` is one candidate and no judges. It reports `SKIP` rather than a verdict, because a run that checked nothing cannot call anything verified.

Solutions are written under `content/solutions/<lang>/problems/`, which is a different tree from the scanned corpus. What the magazine printed is source material and what this repository worked out is not, so a reader who wants the archive can take `content/<lang>` and leave the machine's answers behind. It also means the solver can be rerun over a whole set without touching a byte of the scans, which is what makes a bad run cheap to throw away.

Each solution file records how far it was checked, because a solution sitting in an archive that does not say that is asking to be quoted as though it were the magazine's own.
