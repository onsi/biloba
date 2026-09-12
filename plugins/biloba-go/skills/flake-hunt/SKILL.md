---
name: flake-hunt
description: Run a flake hunt on a Biloba Ginkgo suite — run the whole browser suite many times (60 by default), every run to completion with its own JSON report, then read each spec's failure rate, the seeds, and Biloba's failure evidence out of those reports instead of re-running. Covers the hunt script (compile once, --procs at the measured knee, --randomize-all, --poll-progress-after, a per-run BILOBA_SCREENSHOTS_DIR), why --repeat and --until-it-fails can't measure a rate, what keeps a hunt a valid measurement (one hunt at a time on an idle machine, nothing editing the tree, focused hunts for iterating only, stale visual baselines), how many clean runs it takes to call a flake dead, reading the results (one systemic race behind many names, wedges, a long tail that clusters by run), and the performance record a hunt produces (spec timing, parallel efficiency, per-spec cost, drift between hunts). Use before declaring a flake fixed, after changing shared test helpers or fixtures, at the end of a batch of work, or to measure a suite's performance.
---

# Flake hunts

A flake hunt measures how often each spec fails. It runs the whole suite many times, lets every run finish, and keeps one JSON report per run. You read the reports afterwards to get a failure rate per spec, and you diagnose each failure from the evidence in its report rather than by trying to reproduce it.

Fixing what a hunt finds → `flaky-specs`. Reading a single failure's artifacts → `debug-failures`. Sibling skills are named here without a prefix; invoke one with the same plugin prefix you loaded this skill under.

## When to hunt

- **Before calling a flake fixed**, and again after every fix. Fixing the line that failed lets the spec reach later lines that never ran under failure, and they can have races of their own.
- **After changing shared test helpers, fixtures, the suite bootstrap**, or anything timing-sensitive.
- **At the end of a batch of work**, as sign-off. A hunt takes 60 times the suite's wall clock, so batch changes and hunt once rather than after every edit.
- **To take a performance snapshot** (see [Performance](#performance-from-the-same-reports)).

The gate for an ordinary change is still one `ginkgo -r -p` run. A hunt measures a rate; it doesn't replace the gate.

## The hunt script

```bash
#!/usr/bin/env bash
# Run the browser suite RUNS times, every run to completion, one JSON report per run.
set -uo pipefail
PKG=${PKG:-./e2e}
RUNS=${RUNS:-60}
PROCS=${PROCS:-6}                    # your measured knee (see "Choosing --procs")
DIR=${DIR:-.ginkgo-report/flake}
GINKGO="go run github.com/onsi/ginkgo/v2/ginkgo"

rm -rf "$DIR" && mkdir -p "$DIR"
DIR=$(cd "$DIR" && pwd)              # absolute: the test binary runs in its package directory
$GINKGO build "$PKG" >/dev/null || exit 1
BIN="$PKG/$(basename "$PKG").test"   # compiled once; every run replays it

failed=0
for i in $(seq 1 "$RUNS"); do
  n=$(printf '%03d' "$i")
  if BILOBA_SCREENSHOTS_DIR="$DIR/run-$n" $GINKGO --procs="$PROCS" --randomize-all --no-color \
      --poll-progress-after=10s \
      ${LABELS:+--label-filter="$LABELS"} ${FOCUS_FILE:+--focus-file="$FOCUS_FILE"} \
      --json-report="run-$n.json" --output-dir="$DIR" \
      "$BIN" >"$DIR/run-$n.log" 2>&1; then
    echo "run $n/$RUNS  ok"
  else
    failed=$((failed + 1)); echo "run $n/$RUNS  FAIL  ($DIR/run-$n.log)"
  fi
done
echo "$failed of $RUNS runs failed"
[ "$failed" -eq 0 ]
```

Wrap it in a `make flake-hunt` target if the project has a Makefile, and gitignore the report directory. What each choice is for:

- **Every run completes.** `ginkgo --repeat` and `--until-it-fails` stop at the first failure. That tells you something flaked but not how often, and the first frequent flake hides every rarer one behind it. The loop keeps going and exits non-zero at the end if any run failed.
- **Compiled once.** Every run replays the same test binary, so 60 runs aren't 60 compiles and the code under test can't change halfway through. Build any embedded frontend assets before starting.
- **One JSON report per run.** Each report records the run's `RandomSeed`, each failure's message and `file:line`, the spec's `GinkgoWriter` output (where Biloba streams `console.log`), and Biloba's failure artifacts as report entries: the poll trajectory, the DOM outline, the screenshot path, and the detached-node, occluded-click, and shadowed-handler diagnoses when they apply.
- **A separate `BILOBA_SCREENSHOTS_DIR` per run.** Failure screenshots are named after the spec and the tab, so in a shared directory a later run overwrites an earlier run's screenshot of the same spec. An explicit `BilobaConfigScreenshotsToDir` in the bootstrap takes precedence over the variable, so remove it or have it read the variable.
- **`--poll-progress-after=10s`.** A spec still running after 10s gets a progress report with a goroutine dump and a screenshot of every open tab. That's the evidence you need for a hang.
- **`LABELS` and `FOCUS_FILE`** narrow a hunt without editing the script (`LABELS='!visual'`, `FOCUS_FILE=checkout`). Focus by file. A substring `--focus` into an `Ordered` container runs the matched spec without the earlier specs whose state it depends on.

## Choosing `--procs`

A hunt runs in parallel because concurrency widens race windows. Beyond a point, more processes only add contention. A Biloba spec spends most of its time waiting for the one shared Chrome to render and lay out: in one measured suite Chrome used about 4.7 cores while the Go processes stayed near 2% CPU. Once Chrome saturates the machine's performance cores, each extra process slows every round trip and produces timeouts that come from the machine, not from the code.

Find the knee once per machine: run the suite at 2, 4, 6, and 8 processes and record the wall clock. It falls roughly linearly and then flattens. Hunt and gate at the point where it flattens. In the suite above, one Chrome per process and a RAM disk for temp files both made no difference, because the limit was Chrome's render CPU.

## Keeping the measurement valid

- **Run one hunt at a time, on an otherwise idle machine.** Two hunts at once, or a hunt alongside a build, another test run, or a second agent, overload the cores and fail unrelated specs across the suite. Those failures say nothing about the code. A quiet machine can also hide a race, so only compare rates from hunts taken under the same conditions.
- **Nothing edits the tree during a hunt.** Compiling once protects the Go code, but not fixtures or assets read from disk at run time, and not a `git stash` from another process. Two signs a run was contaminated rather than flaky: many unrelated specs fail in one run and pass in the next, or the log has no `SUCCESS!`/`FAIL!` verdict line. Discard those runs.
- **A focused hunt can't confirm a fix.** `FOCUS_FILE` puts every process behind a handful of specs. That gives heavy contention on one axis and none of the cross-file interleaving that `--randomize-all` produces across the whole suite. In one case, a shared-helper refactor passed 20 out of 20 focused runs and then flaked repeatedly in full-suite runs. For changes to shared helpers, fixtures, or a family of specs, only a full-suite hunt counts.
- **Run the suite once before hunting if visual specs might be stale.** A failing `HaveScreenshot` uses its entire timeout before it fails, because every poll captures and compares again. One visual flake in 60 runs costs little. A stale baseline fails every visual spec on every run and turns a hunt of minutes into hours. Whether to include visual specs is a question of their summed spec time, not wall clock; if they carry a label, `LABELS='!visual'` excludes them.

## Reading the result

### Failure rate

```bash
#!/usr/bin/env bash
# Per-spec failure rate across a hunt's reports, with the runs and seeds to look at.
DIR=${DIR:-.ginkgo-report/flake}
for log in "$DIR"/run-*.log; do
  [ -f "${log%.log}.json" ] || echo "!! $(basename "$log" .log) left no report (killed?) — not a clean run"
done
jq -rn '
  [inputs | .[0] as $s | {
    run: (input_filename | sub(".*/"; "") | sub("\\.json$"; "")),
    seed: $s.SuiteConfig.RandomSeed,
    reasons: ($s.SpecialSuiteFailureReasons // []),
    failures: [$s.SpecReports[] | select(.State | IN("failed", "panicked", "timedout", "aborted")) | {
      spec: (.LeafNodeType as $type | (.ContainerHierarchyTexts + [.LeafNodeText])
             | map(select(. != "")) | join(" > ") | if . == "" then "(\($type))" else . end),
      at: "\(.Failure.Location.FileName | sub(".*/"; "")):\(.Failure.Location.LineNumber)"}]}]
  | (map(select(.reasons | length > 0))) as $unfinished
  | "\(length) runs: \(map(select(.failures | length > 0)) | length) with a failure, \($unfinished | length) unfinished",
    ($unfinished[] | "!! \(.run) did not finish (\(.reasons | join("; "))) — not a clean run"),
    "",
    ([.[] | .run as $run | .seed as $seed | .failures[] | . + {run: $run, seed: $seed}]
     | group_by(.spec) | sort_by(-length)[]
     | "\(length)x  \(.[0].spec)\n      at \(.[0].at)   runs: \(map(.run) | join(", "))   seeds: \(map(.seed) | join(", "))")
' "$DIR"/run-*.json
```

```
60 runs: 3 with a failure, 0 unfinished

2x  checkout > applies the coupon
      at checkout_test.go:88   runs: run-014, run-051   seeds: 1789182212, 1789182260
1x  (SynchronizedBeforeSuite)
      at e2e_suite_test.go:31   runs: run-033   seeds: 1789182244
```

A run that was interrupted, timed out, or failed to compile carries `SpecialSuiteFailureReasons`, and a run that was killed leaves no report. Neither is a clean run. A truncated run had less chance to flake, so counting it toward a clean streak skews the result toward "fixed".

### One failure's evidence

```bash
jq -r --arg spec "applies the coupon" '
  .[0].SpecReports[]
  | select((.State | IN("failed", "panicked", "timedout", "aborted")) and (.LeafNodeText | contains($spec)))
  | "== \(input_filename)\n\(.Failure.Message)\n"
    + (.ReportEntries // [] | map("-- \(.Name)\n\(.Value.Representation)") | join("\n"))
    + "\n-- GinkgoWriter\n\(.CapturedGinkgoWriterOutput // "")"
' .ginkgo-report/flake/run-*.json
```

That prints the failure message, Biloba's report entries (`Poll trajectory`, `DOM Outline for: …`, `Screenshot for: …`, and the others), and the spec's output. The screenshots are in `run-NNN/`, and a hang's progress report is in `run-NNN.log`. How to read each artifact → `debug-failures`.

**Work from the evidence; don't try to reproduce the failure.** Re-running a failed seed rarely reproduces a race: the seed fixes spec order, but not which process runs each spec or how the browser schedules its work. A rerun usually comes back green, which proves nothing. Form a hypothesis about the mechanism from the message, trajectory, outline, screenshot, and output, fix it for a reason you can state, and hunt again. When a failure does look order-dependent, the seed is worth a try: `ginkgo --seed=<seed> --randomize-all --procs=<N> ./e2e`.

Don't paper over a flake by widening a timeout, adding `FlakeAttempts`, or re-running until green. The fixes for each smell → `flaky-specs`.

### Patterns worth recognizing

- **Different specs failing in each run means one systemic race.** If a few hunts produce several distinct failing specs with no repeats, all in one area of the app, the named specs are just the ones that happened to be running when a shared mechanism misfired. Stop fixing names and find the mechanism. In one suite it was a component remounting and resetting its local state, which undid any click made before the tree settled.
- **A timeout far over its budget means a call into the browser stalled.** Gomega checks its deadline between polls, so `Timed out after 8.3s` on a 2s `Eventually` means a single call into the browser took seconds to return. The trajectory shows few samples, and the failure screenshot can show the right state because the page caught up after the assertion gave up. To show whether the tab's event loop was running during the gap, find something the tab itself produced, such as a request it made with a server-side timestamp; a goroutine dump only shows that Go was waiting. Biloba bounds each call into Chrome, so a wedge longer than 30s fails with `deadline_exceeded` instead → `debug-failures`.
- **A flat trajectory in a rare failure is a race that left the page in the wrong state.** The value was computed once and never corrected, so a longer timeout won't help. The race decides which path the app takes; once on the wrong path, the result is the same every time. Compare the failing run's outline and screenshot with what a passing run shows.

### How many clean runs mean a flake is dead

A flake that fails in a fraction `p` of runs still passes `n` runs in a row with probability `(1 − p)^n`:

| Rate | 30 clean runs happen by chance | 60 clean runs happen by chance |
|---|---|---|
| 5% | 21% | 4.6% |
| 3% | 40% | 16% |

So 30 green runs tell you little about a flake that fires a few percent of the time. To call a flake at rate `p` dead with about 95% confidence you need roughly `3 / p` clean runs: 60 for 5%, 100 for 3%. You also need a written root cause. If you can't say why it flaked, the clean runs may just be luck.

## Performance from the same reports

Every report records every spec's duration, so a hunt also gives you 60 timing samples per spec instead of one.

```bash
#!/usr/bin/env bash
# Where a hunt's time went: wall clock, parallel efficiency, slowest specs, the long tail.
DIR=${DIR:-.ginkgo-report/flake}
jq -rn '
  def median: sort | if length == 0 then 0 elif length % 2 == 1 then .[length / 2 | floor]
                     else (.[length / 2 - 1] + .[length / 2]) / 2 end;
  def r: . * 100 | round / 100;
  [inputs | (input_filename | sub(".*/"; "") | sub("\\.json$"; "")) as $run | .[0]
   | select((.SpecialSuiteFailureReasons // []) | length == 0)          # unfinished runs flatter every number
   | {run: $run, wall: (.RunTime / 1e9), procs: .SuiteConfig.ParallelTotal,
      specs: [.SpecReports[] | select(.LeafNodeType == "It" and .State != "skipped" and .State != "pending") | {
        run: $run, state: .State, t: (.RunTime / 1e9),
        name: ((.ContainerHierarchyTexts + [.LeafNodeText]) | join(" > ")),
        setup: ([.SpecEvents[]? | select(.SpecEventType == "Node (End)" and .NodeType == "BeforeAll")
                 | .Duration] | add // 0 | . / 1e9)}]}]
  | (map(.wall) | median) as $wall | length as $n
  | (map(.specs | map(.t) | add) | add / $n) as $summed
  | ([.[].specs[] | select(.state == "passed")] | group_by(.name) | map({
      name: .[0].name, med: (map(.t) | median), setup: (map(.setup) | median),
      max: (max_by(.t) | .t), maxrun: (max_by(.t) | .run)})) as $specs
  | ($specs | map(select(.med > 0.01 and .max >= 1 and .max / .med >= 3))) as $tail
  | "\($n) finished runs at \(.[0].procs) procs, \($specs | length) specs",
    "wall clock  median \($wall | r)s  min \(map(.wall) | min | r)s  max \(map(.wall) | max | r)s",
    "summed spec time \($summed | r)s per run; parallel efficiency \($summed / .[0].procs / $wall | r)",
    "per-spec cost \(($specs | map(.med) | add) / ($specs | length) * 1000 | round)ms (sum of medians / spec count)",
    "", "slowest by median (setup = BeforeAll, charged to the first spec of its Ordered container):",
    ($specs | sort_by(-.med)[:15][] | "  \(.med | r)s  setup \(.setup | r)s  \(.name)"),
    "", "long tail (worst run >= 3x the spec'"'"'s own median, and >= 1s):",
    (if ($tail | length) == 0 then "  none" else empty end),
    ($tail | sort_by(-(.max / .med))[] | "  \(.max / .med | r)x  median \(.med | r)s  max \(.max | r)s  [\(.maxrun)]  \(.name)"),
    "", "tail maxima by run (\($tail | length / $n | r) expected per run if independent):",
    ($tail | group_by(.maxrun) | sort_by(-length)[] | "  \(length)  \(.[0].maxrun)")
' "$DIR"/run-*.json
```

How to read it:

- **Wall clock spread is your noise floor.** In one suite it was about 6% from run to run with no code change. Treat any change of that size as noise.
- **Parallel efficiency** is summed spec time divided by processes, divided by wall clock. When it's high, the wall clock is about summed time divided by processes, so saving N spec-seconds saves N / procs wall-clock seconds. Do that division before deleting slow specs to speed up the suite; the result is usually smaller than it looks.
- **`setup` separates a spec from its container.** A `BeforeAll` is charged to whichever spec runs first in its `Ordered` container, so the slowest spec may just be paying for the container's setup.
- **Slow outliers cluster by run.** In one 60-run hunt, 42 tail maxima landed in 21 runs, and one run held 7 where chance predicts fewer than one. Several unrelated specs spiking in the same run, often in different processes within the same few seconds, is a fact about that run (a stalled machine, browser, or tab), not about those specs. Check the run column before filing a spec as slow. The ratio also favors specs with tiny medians: a 20ms spec that once took 4s shows up as 200×.
- **Don't assign a cause to a slow episode without evidence that tells the layers apart.** The reports record no host metrics and no browser state. A host sampler running during the hunt, or `--poll-progress-after=1s` so short stalls also dump stacks, can separate them.

### Keep a performance record

End each hunt with a snapshot, committed with the work the hunt gated:

```bash
#!/usr/bin/env bash
# One row per hunt in perf/history.tsv, plus the per-spec medians it came from. Commit both.
DIR=${DIR:-.ginkgo-report/flake}
detail="perf/snapshots/$(date +%F)-$(git rev-parse --short HEAD).tsv"
mkdir -p perf/snapshots
[ -f perf/history.tsv ] ||
  printf 'date\tsha\tbiloba\tprocs\truns\tspecs\twall_median_s\tsummed_s\tper_spec_ms\tefficiency\n' > perf/history.tsv
PRELUDE='
  def median: sort | if length % 2 == 1 then .[length / 2 | floor] else (.[length / 2 - 1] + .[length / 2]) / 2 end;
  def r: . * 1000 | round / 1000;
  [inputs | .[0] | select((.SpecialSuiteFailureReasons // []) | length == 0)] as $runs
  | ([$runs[].SpecReports[] | select(.LeafNodeType == "It" and .State == "passed")
      | {name: ((.ContainerHierarchyTexts + [.LeafNodeText]) | join(" > ")), t: (.RunTime / 1e9)}]
     | group_by(.name) | map({name: .[0].name, med: (map(.t) | median)})) as $specs
  | $runs'
jq -rn "$PRELUDE"' | $specs[] | select(.med >= 0.5) | [(.med * 1000 | round), .name] | @tsv' \
  "$DIR"/run-*.json > "$detail"
jq -rn --arg date "$(date +%F)" --arg sha "$(git rev-parse --short HEAD)" \
  --arg biloba "$(go list -m -f '{{.Version}}' github.com/onsi/biloba)" "$PRELUDE"'
  | length as $n | .[0].SuiteConfig.ParallelTotal as $procs | (map(.RunTime / 1e9) | median) as $wall
  | (map([.SpecReports[] | select(.LeafNodeType == "It") | .RunTime] | add / 1e9) | add / $n) as $summed
  | [$date, $sha, $biloba, $procs, $n, ($specs | length), ($wall | r), ($summed | r),
     (($specs | map(.med) | add) / ($specs | length) * 1000 | r), ($summed / $procs / $wall | r)] | @tsv
' "$DIR"/run-*.json >> perf/history.tsv
column -t -s $'\t' perf/history.tsv | tail -n 2
```

A growing suite's wall clock can't tell "we added specs" from "every spec got slower". Per-spec cost can. To compare the newest row with an earlier one:

- **Per-spec cost** moving more than the noise floor means the specs themselves got slower or faster.
- **The non-linear check:** the baseline's wall clock × (current spec count / baseline spec count) is what the wall clock should be if only the spec count changed. An actual wall clock well above that points to a change with an outsized effect.
- **Diff the detail files** to see which specs entered or left the ≥500ms list and which moved most.
- **Check the `runs` column.** A snapshot from one run is much weaker evidence than one from sixty.

Measure your own noise floor by taking two snapshots with no code change between them.

**Divide before you attribute a slowdown.** Take the size of the effect, divide it by the per-operation cost of the mechanism you suspect, and check that the number of operations is plausible. For scale: a bare round trip to Chrome costs about 0.2ms, a spec in Biloba's own suite sends about 12 commands to Chrome, and `b.NewTab()` costs about 41ms. If an attribution needs thousands of operations per spec, it's wrong, and you should measure instead. "Unexplained" is an acceptable finding.

## When agents do the work

- **One process owns hunts**, and runs them only when no other agent is editing the tree. Agents can run the normal gate for their own work, but a hunt inside a subagent costs many minutes, measures only the area that agent touched, and competes with everything else for the CPU.
- **A hunt outlasts ordinary tool timeouts.** Run it in the background or with the longest timeout available, and read the reports when it finishes.
- **Send a flake back with its evidence.** The agent whose change introduced a flake still has the context, so give it the report excerpt and your hypothesis about the mechanism, and let it disagree if the evidence says otherwise.
