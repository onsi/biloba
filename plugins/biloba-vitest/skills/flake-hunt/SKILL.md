---
name: flake-hunt
description: Run a flake hunt on a Biloba Vitest suite — run the whole browser suite many times (60 by default), every run to completion with its own JSON report, then read each test's failure rate, the seeds, and the failure evidence out of those reports instead of re-running. Covers the hunt script (vitest run with shuffle and a recorded seed, maxWorkers at the measured knee, no retry or bail, a per-run BILOBA_SCREENSHOTS_DIR, and the seed, wall clock, and exit status the JSON reporter doesn't record), the runs the JSON reports as clean when they aren't (unhandled errors, failed beforeAll hooks, killed runs), logging BilobaError's code and trajectory, what keeps a hunt a valid measurement (one hunt at a time on an idle machine, nothing editing the tree, focused hunts for iterating only, stale visual baselines), how many clean runs it takes to call a flake dead, reading the results (one systemic race behind many names, stalled tabs, a long tail that clusters by run), and the performance record a hunt produces (test timing, the longest file, per-test cost, drift between hunts). Use before declaring a flake fixed, after changing shared test helpers or fixtures, at the end of a batch of work, or to measure a suite's performance.
---

# Flake hunts

A flake hunt measures how often each test fails. It runs the whole suite many times, lets every run finish, and keeps one JSON report and one log per run. You read them afterwards to get a failure rate per test, and you diagnose each failure from the evidence it left rather than by trying to reproduce it.

Fixing what a hunt finds → `biloba-vitest:flaky-tests`. Reading a single failure → `biloba-vitest:debug-failures`.

## When to hunt

- **Before calling a flake fixed**, and again after every fix. Fixing the line that failed lets the test reach later lines that never ran under failure, and they can have races of their own.
- **After changing shared test helpers, fixtures, global setup**, or anything timing-sensitive.
- **At the end of a batch of work**, as sign-off. A hunt takes 60 times the suite's wall clock, so batch changes and hunt once rather than after every edit.
- **To take a performance snapshot** (see [Performance](#performance-from-the-same-reports)).

The gate for an ordinary change is still one `vitest run`. A hunt measures a rate; it doesn't replace the gate.

## The hunt script

```bash
#!/usr/bin/env bash
# Run the browser suite RUNS times, every run to completion, one JSON report per run.
set -uo pipefail
RUNS=${RUNS:-60}
WORKERS=${WORKERS:-6}                # your measured knee (see "Choosing maxWorkers")
DIR=${DIR:-.vitest-report/flake}
now() { node -p 'Date.now()'; }

rm -rf "$DIR" && mkdir -p "$DIR"
DIR=$(cd "$DIR" && pwd)

failed=0
for i in $(seq 1 "$RUNS"); do
  n=$(printf '%03d' "$i"); seed=$((RANDOM * 32768 + RANDOM)); start=$(now)
  BILOBA_SCREENSHOTS_DIR="$DIR/run-$n" npx vitest run --retry=0 --bail=0 \
    --sequence.shuffle --sequence.seed="$seed" --maxWorkers="$WORKERS" \
    --reporter=default --reporter=json --outputFile.json="$DIR/run-$n.json" \
    ${FILES:-} >"$DIR/run-$n.log" 2>&1
  status=$?
  if [ -f "$DIR/run-$n.json" ]; then       # the JSON has no seed, wall clock, exit status, or worker count
    jq --argjson seed "$seed" --argjson wallMs "$(($(now) - start))" --argjson exit "$status" \
      --argjson workers "$WORKERS" '. + {hunt: {seed: $seed, wallMs: $wallMs, exit: $exit, workers: $workers}}' \
      "$DIR/run-$n.json" > "$DIR/tmp.json" && mv "$DIR/tmp.json" "$DIR/run-$n.json"
  fi
  if [ "$status" -eq 0 ]; then echo "run $n/$RUNS  ok"
  else failed=$((failed + 1)); echo "run $n/$RUNS  FAIL  ($DIR/run-$n.log)"; fi
done
echo "$failed of $RUNS runs failed"
[ "$failed" -eq 0 ]
```

Add it as a `package.json` script if that's how the project runs things, and gitignore the report directory. What each choice is for:

- **Every run completes, and nothing retries.** `--bail` stops at the first failure, which tells you something flaked but not how often; the first frequent flake then hides every rarer one behind it. `--retry` passes a flaky test on its second attempt, which is exactly the failure you're trying to count. Both are forced off here in case the config sets them. The loop keeps going and exits non-zero at the end if any run failed.
- **Shuffled, with a seed you chose.** `--sequence.shuffle` randomizes file and test order so order dependence shows up. The JSON reporter doesn't record the seed, so the loop picks one, passes it in, and writes it into the report along with the wall clock, exit status, and worker count.
- **`vitest run`, never watch mode.** Build the app and any assets before starting, so nothing rebuilds partway through the hunt.
- **Both reporters.** The JSON report has each test's status, duration, and failure message. The log has everything the JSON leaves out: unhandled errors, hook failures, and the diagnostics `installBilobaVitestHooks` writes to stderr after a failure (screenshots and outlines of every live tab). Keep both.
- **A separate `BILOBA_SCREENSHOTS_DIR` per run**, so one run's screenshots don't overwrite another's. The variable is only read when `connect` isn't given an explicit `artifactDir` or `diagnostics.artifactDir`, so remove that option or have it read the variable.
- **`FILES`** narrows a hunt to some files without editing the script (`FILES=test/checkout.test.ts`), and `FILES='--exclude=**/visual*'` drops some (keep the `=`, so the shell leaves the glob alone).

Two additions to the suite make the reports more useful. Set `includeTaskLocation: true` in the Vitest config so each test in the JSON carries its line number. And add a setup file that logs the `BilobaError` fields the JSON reporter drops. The reporter keeps only the message and stack, not `code` or `trajectory`:

```ts
// test/hunt-diagnostics.ts, listed in setupFiles
import {afterEach} from "vitest";

afterEach(({task}) => {
  for (const error of task.result?.errors ?? []) {
    const {code, trajectory} = error as {code?: string; trajectory?: unknown};
    if (code) console.error(`[biloba] ${task.name}: ${code}\n${JSON.stringify(trajectory, null, 1)}`);
  }
});
```

Pass `progressAfterMs` to `installBilobaVitestHooks` as well, so a test that hangs writes a capture of every live tab to the log while it's still hanging.

## Choosing `maxWorkers`

A hunt runs in parallel because concurrency widens race windows. Beyond a point, more workers only add contention. Each worker drives its own tab in the one shared Chrome and spends most of its time waiting for Chrome to render and lay out: in one measured suite Chrome used about 4.7 cores while the processes driving it stayed near 2% CPU. Once Chrome saturates the machine's performance cores, each extra worker slows every round trip and produces timeouts that come from the machine, not from the code.

Find the knee once per machine: run the suite at 2, 4, 6, and 8 workers and record the wall clock. It falls roughly linearly and then flattens. Hunt and gate at the point where it flattens. A worker takes one file at a time, so the knee also depends on how evenly the suite's time is spread across files (see [the longest file](#performance-from-the-same-reports)).

## Keeping the measurement valid

- **Run one hunt at a time, on an otherwise idle machine.** Two hunts at once, or a hunt alongside a build, another test run, or a second agent, overload the cores and fail unrelated tests across the suite. Those failures say nothing about the code. A quiet machine can also hide a race, so only compare rates from hunts taken under the same conditions.
- **Nothing edits the tree during a hunt.** Each run transforms the test files again from disk, so an edit, a rebuild, or a `git stash` from another process changes what later runs test. Two signs a run was contaminated rather than flaky: many unrelated tests fail in one run and pass in the next, or the log stops before Vitest's summary. Discard those runs.
- **A focused hunt can't confirm a fix.** Narrowing `FILES` puts every worker behind a handful of tests. That gives heavy contention on one axis and none of the interleaving a shuffled full suite produces. In one case, a shared-helper refactor passed 20 out of 20 focused runs and then flaked repeatedly in full-suite runs. For changes to shared helpers, fixtures, or a family of tests, only a full-suite hunt counts. Narrow by file rather than with `-t`: a name filter can skip the earlier tests in a file whose state a later test depends on.
- **Run the suite once before hunting if visual tests might be stale.** A failing `expectScreenshot` uses its entire timeout before it fails, because every poll captures and compares again. One visual flake in 60 runs costs little. A stale baseline fails every visual test on every run and turns a hunt of minutes into hours. Whether to include visual tests is a question of their summed test time, not wall clock.

## Reading the result

### Failure rate

```bash
#!/usr/bin/env bash
# Per-test failure rate across a hunt's reports, with the runs and seeds to look at.
DIR=${DIR:-.vitest-report/flake}
for log in "$DIR"/run-*.log; do
  [ -f "${log%.log}.json" ] || echo "!! $(basename "$log" .log) left no report (interrupted or killed) — not a clean run"
  grep -q "unhandled error" "$log" && echo "!! $(basename "$log" .log) had an unhandled error, which its JSON does not record — read its log"
done
jq -rn '
  [inputs | {
    run: (input_filename | sub(".*/"; "") | sub("\\.json$"; "")), seed: .hunt.seed,
    failed: (.success == false or .hunt.exit != 0),
    failures: [.testResults[] | (.name | sub(".*/"; "")) as $file
      | [.assertionResults[] | select(.status == "failed")] as $failed
      | if $failed | length > 0 then $failed[] | {
            spec: ((.ancestorTitles + [.title]) | join(" > ")),
            at: "\($file)\(if .location then ":\(.location.line)" else "" end)"}
        elif .status == "failed" then {spec: "(\($file): a hook failed or the file did not load)", at: $file}
        else empty end]}]
  | map(select(.failed and (.failures | length) == 0)) as $silent
  | "\(length) runs: \(map(select(.failures | length > 0)) | length) with a failure, \($silent | length) failed with nothing reported",
    ($silent[] | "!! \(.run) exited non-zero with no failed test or file (an unhandled error?) — read its log"),
    "",
    ([.[] | .run as $run | .seed as $seed | .failures[] | . + {run: $run, seed: $seed}]
     | group_by(.spec) | sort_by(-length)[]
     | "\(length)x  \(.[0].spec)\n      at \(.[0].at)   runs: \(map(.run) | join(", "))   seeds: \(map(.seed) | join(", "))")
' "$DIR"/run-*.json
```

```
60 runs: 3 with a failure, 1 failed with nothing reported
!! run-027 exited non-zero with no failed test or file (an unhandled error?) — read its log

2x  checkout > applies the coupon
      at checkout.test.ts:88   runs: run-014, run-051   seeds: 448334667, 819785760
1x  (cart.test.ts: a hook failed or the file did not load)
      at cart.test.ts   runs: run-033   seeds: 71648498
```

The report has to look past the JSON because the JSON calls some failed runs clean:

- **An unhandled error** (a rejected promise nobody awaited, an error thrown from a timer) makes Vitest exit non-zero, but the JSON still says `success: true` with every test passed. Only the exit status and the log show it. A flake that surfaces this way has usually escaped its test; the log names the error.
- **A failed `beforeAll`** marks the file's tests `skipped`, not `failed`, and the hook's error isn't in the JSON at all. The report lists the file; the error is in the log.
- **An interrupted or killed run** leaves no JSON. It isn't a clean run: it had less chance to flake, so counting it toward a clean streak skews the result toward "fixed".

### One failure's evidence

For a failing test, collect the JSON's `failureMessages`, the lines the diagnostics setup file wrote (`[biloba] <test>: TIMEOUT` and the trajectory), the capture `installBilobaVitestHooks` wrote after the failure, and the screenshots in `run-NNN/`:

```bash
jq -r --arg test "applies the coupon" '
  .testResults[].assertionResults[] | select(.status == "failed" and (.title | contains($test)))
  | "== \(input_filename)\n\(.failureMessages | join("\n"))"
' .vitest-report/flake/run-*.json
grep -n -A30 "\[biloba\] applies the coupon" .vitest-report/flake/run-*.log
```

What the codes, trajectories, and captures mean → `biloba-vitest:debug-failures`.

**Work from the evidence; don't try to reproduce the failure.** Re-running a failed seed rarely reproduces a race: the seed fixes test order, but not which worker runs each file or how the browser schedules its work. A rerun usually comes back green, which proves nothing. Form a hypothesis about the mechanism from the message, trajectory, outline, screenshot, and log, fix it for a reason you can state, and hunt again. When a failure does look order-dependent, the seed is worth a try: `npx vitest run --sequence.shuffle --sequence.seed=<seed> --maxWorkers=<N>`.

Don't paper over a flake by widening a timeout, adding `retry`, or re-running until green. The fixes for each smell → `biloba-vitest:flaky-tests`.

### Patterns worth recognizing

- **Different tests failing in each run means one systemic race.** If a few hunts produce several distinct failing tests with no repeats, all in one area of the app, the named tests are just the ones that happened to be running when a shared mechanism misfired. Stop fixing names and find the mechanism. In one suite it was a component remounting and resetting its local state, which undid any click made before the tree settled.
- **A long gap in a trajectory means a call into the browser stalled.** When the polling interval is milliseconds and the trajectory shows no attempt for seconds, one call into the browser didn't come back. The failure capture can show the right state because the page caught up after the assertion gave up. To show whether the tab's event loop was running during the gap, find something the tab itself produced, such as a request it made with a server-side timestamp.
- **A flat trajectory in a rare failure is a race that left the page in the wrong state.** The value was computed once and never corrected, so a longer timeout won't help. The race decides which path the app takes; once on the wrong path, the result is the same every time. Compare the failing run's capture with what a passing run shows.
- **`PAGE_CRASHED`, `BROWSER_GONE`, and `DRIVER_CLOSED`** name the layer that died: the tab's renderer, the shared Chrome, or the worker's daemon. A crash that takes down several tests in one run counts as one event in that run, not as several flakes.

### How many clean runs mean a flake is dead

A flake that fails in a fraction `p` of runs still passes `n` runs in a row with probability `(1 − p)^n`:

| Rate | 30 clean runs happen by chance | 60 clean runs happen by chance |
|---|---|---|
| 5% | 21% | 4.6% |
| 3% | 40% | 16% |

So 30 green runs tell you little about a flake that fires a few percent of the time. To call a flake at rate `p` dead with about 95% confidence you need roughly `3 / p` clean runs: 60 for 5%, 100 for 3%. You also need a written root cause. If you can't say why it flaked, the clean runs may just be luck.

## Performance from the same reports

Every report records every test's duration, so a hunt also gives you 60 timing samples per test instead of one.

```bash
#!/usr/bin/env bash
# Where a hunt's time went: wall clock, parallel efficiency, the long-pole files, the long tail.
DIR=${DIR:-.vitest-report/flake}
jq -rn '
  def median: sort | if length == 0 then 0 elif length % 2 == 1 then .[length / 2 | floor]
                     else (.[length / 2 - 1] + .[length / 2]) / 2 end;
  def r: . * 100 | round / 100;
  [inputs | (input_filename | sub(".*/"; "") | sub("\\.json$"; "")) as $run
   | {run: $run, wall: (.hunt.wallMs / 1000), workers: .hunt.workers,
      files: [.testResults[] | {name: (.name | sub(".*/"; "")), t: ((.endTime - .startTime) / 1000)}],
      tests: [.testResults[] | (.name | sub(".*/"; "")) as $file | .assertionResults[]
              | select(.status == "passed" or .status == "failed")
              | {run: $run, state: .status, t: ((.duration // 0) / 1000),
                 name: ([$file] + .ancestorTitles + [.title] | join(" > "))}]}]
  | (map(.wall) | median) as $wall | length as $n | .[0].workers as $workers
  | (map(.files | map(.t) | add) | add / $n) as $fileTime
  | ([.[].tests[] | select(.state == "passed")] | group_by(.name) | map({
      name: .[0].name, med: (map(.t) | median),
      max: (max_by(.t) | .t), maxrun: (max_by(.t) | .run)})) as $tests
  | ($tests | map(select(.med > 0.01 and .max >= 1 and .max / .med >= 3))) as $tail
  | "\($n) runs at \($workers) workers, \($tests | length) tests",
    "wall clock  median \($wall | r)s  min \(map(.wall) | min | r)s  max \(map(.wall) | max | r)s",
    "summed file time \($fileTime | r)s per run; parallel efficiency \($fileTime / $workers / $wall | r)",
    "  (file time spans tests only: beforeAll/afterAll, connect(), and imports are not in the JSON)",
    "per-test cost \(($tests | map(.med) | add) / ($tests | length) * 1000 | round)ms (sum of medians / test count)",
    "", "files by median time (a worker runs one file at a time, so the longest is a floor on wall clock):",
    ([.[].files[]] | group_by(.name) | map({name: .[0].name, med: (map(.t) | median)}) | sort_by(-.med)[:10][]
     | "  \(.med | r)s  \(.name)"),
    "", "slowest tests by median:",
    ($tests | sort_by(-.med)[:15][] | "  \(.med | r)s  \(.name)"),
    "", "long tail (worst run >= 3x the test'"'"'s own median, and >= 1s):",
    (if ($tail | length) == 0 then "  none" else empty end),
    ($tail | sort_by(-(.max / .med))[] | "  \(.max / .med | r)x  median \(.med | r)s  max \(.max | r)s  [\(.maxrun)]  \(.name)"),
    "", "tail maxima by run (\($tail | length / $n | r) expected per run if independent):",
    ($tail | group_by(.maxrun) | sort_by(-length)[] | "  \(length)  \(.[0].maxrun)")
' "$DIR"/run-*.json
```

How to read it:

- **Wall clock spread is your noise floor.** In one suite it was about 6% from run to run with no code change. Treat any change of that size as noise.
- **Hooks don't appear in the JSON.** A file's `startTime` and `endTime` cover its tests only, so `beforeAll`, `afterAll`, `connect()`, and module imports are missing. The wall clock, measured by the loop, includes them along with Vitest's startup and global setup. A large gap between the wall clock and summed file time divided by workers is that setup cost plus uneven packing of files onto workers.
- **The longest file sets a floor.** A worker runs one file at a time, so a file that holds a large share of the suite's time keeps one worker busy after the others finish. Splitting it is often the cheapest speedup available. Otherwise, saving N test-seconds saves about N / workers wall-clock seconds; do that division before deleting slow tests to speed up the suite.
- **Slow outliers cluster by run.** In one 60-run hunt, 42 tail maxima landed in 21 runs, and one run held 7 where chance predicts fewer than one. Several unrelated tests spiking in the same run, often in different workers within the same few seconds, is a fact about that run (a stalled machine, browser, or tab), not about those tests. Check the run column before filing a test as slow. The ratio also favors tests with tiny medians: a 20ms test that once took 4s shows up as 200×.
- **Don't assign a cause to a slow episode without evidence that tells the layers apart.** The reports record no host metrics and no browser state. A host sampler running during the hunt, or a short `progressAfterMs` so brief stalls also produce captures, can separate them.

### Keep a performance record

End each hunt with a snapshot, committed with the work the hunt gated:

```bash
#!/usr/bin/env bash
# One row per hunt in perf/history.tsv, plus the per-test medians it came from. Commit both.
DIR=${DIR:-.vitest-report/flake}
detail="perf/snapshots/$(date +%F)-$(git rev-parse --short HEAD).tsv"
mkdir -p perf/snapshots
[ -f perf/history.tsv ] ||
  printf 'date\tsha\tbiloba\tworkers\truns\ttests\twall_median_s\tfile_time_s\tper_test_ms\tefficiency\n' > perf/history.tsv
PRELUDE='
  def median: sort | if length % 2 == 1 then .[length / 2 | floor] else (.[length / 2 - 1] + .[length / 2]) / 2 end;
  def r: . * 1000 | round / 1000;
  [inputs] as $runs
  | ([$runs[].testResults[] | (.name | sub(".*/"; "")) as $file | .assertionResults[]
      | select(.status == "passed") | {name: ([$file] + .ancestorTitles + [.title] | join(" > ")), t: (.duration / 1000)}]
     | group_by(.name) | map({name: .[0].name, med: (map(.t) | median)})) as $tests
  | $runs'
jq -rn "$PRELUDE"' | $tests[] | select(.med >= 0.5) | [(.med * 1000 | round), .name] | @tsv' \
  "$DIR"/run-*.json > "$detail"
jq -rn --arg date "$(date +%F)" --arg sha "$(git rev-parse --short HEAD)" \
  --arg biloba "$(node -p "require('./node_modules/biloba/package.json').version")" "$PRELUDE"'
  | length as $n | .[0].hunt.workers as $workers | (map(.hunt.wallMs / 1000) | median) as $wall
  | (map([.testResults[] | .endTime - .startTime] | add / 1000) | add / $n) as $fileTime
  | [$date, $sha, $biloba, $workers, $n, ($tests | length), ($wall | r), ($fileTime | r),
     (($tests | map(.med) | add) / ($tests | length) * 1000 | r), ($fileTime / $workers / $wall | r)] | @tsv
' "$DIR"/run-*.json >> perf/history.tsv
column -t -s $'\t' perf/history.tsv | tail -n 2
```

A growing suite's wall clock can't tell "we added tests" from "every test got slower". Per-test cost can. To compare the newest row with an earlier one:

- **Per-test cost** moving more than the noise floor means the tests themselves got slower or faster.
- **The non-linear check:** the baseline's wall clock × (current test count / baseline test count) is what the wall clock should be if only the test count changed. An actual wall clock well above that points to a change with an outsized effect.
- **Diff the detail files** to see which tests entered or left the ≥500ms list and which moved most.
- **Check the `runs` column.** A snapshot from one run is much weaker evidence than one from sixty.

Measure your own noise floor by taking two snapshots with no code change between them.

**Divide before you attribute a slowdown.** Take the size of the effect, divide it by the per-operation cost of the mechanism you suspect, and check that the number of operations is plausible. For scale: a bare round trip from the daemon to Chrome costs about 0.2ms, and opening a new tab costs about 41ms. If an attribution needs thousands of operations per test, it's wrong, and you should measure instead. "Unexplained" is an acceptable finding.

## When agents do the work

- **One process owns hunts**, and runs them only when no other agent is editing the tree. Agents can run the normal gate for their own work, but a hunt inside a subagent costs many minutes, measures only the area that agent touched, and competes with everything else for the CPU.
- **A hunt outlasts ordinary tool timeouts.** Run it in the background or with the longest timeout available, and read the reports when it finishes.
- **Send a flake back with its evidence.** The agent whose change introduced a flake still has the context, so give it the report excerpt and your hypothesis about the mechanism, and let it disagree if the evidence says otherwise.
