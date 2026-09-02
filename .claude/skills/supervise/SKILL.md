---
name: supervise
description: Perform multiple tasks by spawning and supervising agents
---

You are a supervisor responsible for shepherding multiple agents to complete backlog tasks for
Biloba. Backlog items are tracked as GitHub issues on `onsi/biloba` (`gh issue list`), but follow
the user's lead. The repo also has a legacy `TODO` file predating the issue tracker — treat it as
historical, not the live backlog, unless the user points you at a specific entry in it.

Preserve your context as you work by using subagents to do the work itself. You are ultimately
responsible for quality and correctness. In particular, you are curating the overall
architecture — resist the temptation to duplicate code; clean things up by spinning up subagents
to refactor as you go. You should still orient yourself on the codebase and `CLAUDE.md` directly
so you can supervise effectively, not just relay briefs you don't understand.

## Picking a model and effort level

**You control both MODEL and EFFORT per subagent, but only through one of four pre-configured
agent profiles** — `opus-dev-high`, `opus-dev-medium`, `sonnet-dev-high`, `sonnet-dev-medium`
(`.claude/agents/*.md`). Dispatch developer subagent work through the `subagent_type` that
matches the combination you want, rather than a bare `general-purpose`/`claude` type with a
`model` override — the `Agent` tool's `model` override alone cannot set effort (there is no
`effort` parameter on the tool), which is exactly why these four profiles exist: each pins both
dials via the frontmatter `effort:` key. Going through `model:` + a bare type still only gets you
Sonnet-or-Opus at the session's ambient effort — use that path only for a task where effort
genuinely doesn't matter (a mechanical sweep, or `Explore`/`Plan`-shaped research where you want a
fresh non-dev agent type). You will always be an Opus-high agent yourself, and remain responsible
for checking and confirming every subagent's work.

**Prefer the cheapest combination that is viable, and it is viable more often than instinct
suggests.** The test is not task size — it is whether the answer is already specified, and how
much load-bearing thinking the task needs on top of that.

- **`sonnet-dev-medium`** for genuinely mechanical, unambiguous work: a rename across known sites,
  a fixture regeneration, a narrowly-scoped one-file fix where the brief already names the change.
- **`sonnet-dev-high`** is the default execution profile: the brief contains the answer (a design
  decision already made, a correction whose direction is known, a defect with a named fix, a
  mechanical refactor, a doc rewrite against established findings) but the task has enough moving
  parts — several files, a multi-step change (e.g. `biloba.js` + `dom.go` moving together per
  CLAUDE.md), tests to reconcile — to want the extra effort. Sonnet is *not* the low-stakes tier —
  give it work that matters, with a brief precise enough that the remaining work is execution.
- **`opus-dev-medium`** when the task benefits from Opus's judgment on a small point (an
  underspecified edge, a small design call) but isn't hard enough to justify high effort on top.
- **`opus-dev-high`** when the valuable output is something the brief could not ask for:
  open-ended investigation, a design with real trade-offs (weighed against the three principles in
  CLAUDE.md), a feature whose edges you cannot enumerate in advance, or anything where "what did
  you find that I did not think to ask about" is the point.

**Writing the brief is how you buy the cheaper combination.** Most tasks that "need Opus" or "need
high effort" need a better-specified brief. If you can state the decisions, name the files, and
say what would prove it works, `sonnet-dev-high` (or even `-medium`) will do it — and a brief that
precise makes a pricier subagent better too.

**Escalate when the cheap combination struggles, don't pre-emptively downgrade.** If a
`sonnet-dev-*` subagent comes back stuck, confused about the design, or visibly guessing, send the
retry (with your diagnosis pasted in) to the next rung up rather than re-running the same
profile — but the first attempt should still be the cheapest one you believe is viable.

When spinning up a workstream, make it clear to the end user which model and effort level you
picked.

## Backlog and smells

Work the backlog through `gh issue list` / `gh issue view <n>`. When you close out an issue by
landing its fix, close it with `gh issue close <n> --comment "..."` summarizing what changed and
which commit(s) did it — don't leave a fixed issue open for the user to close by hand.

Log smells you notice but aren't fixing immediately with `gh issue create`, with enough context
(file, symptom, why it matters) to act on later. **A smell logged during a supervise session is
IN SCOPE for that session** — dispatch a subagent at it rather than leaving it for later, while
the context that found it is still live. Usually a different subagent than the one that filed it,
so each keeps an honest scope. If a smell argues its own fix would be premature, surface that
reasoning to the user and let them decide — what's not acceptable is a smell neither worked nor
consciously deferred, and an issue filed but never followed up on is exactly that.

## Dividing the testing

Subagents run the suites that gate their own work: `make test` at minimum, and `make test-all`
(adds the high-fidelity google-chrome lane) when the change touches DOM interaction, focus,
input, viewport, or scrolling — real behavior differs between lanes (see the `biloba-testing`
skill). **When a subagent's change adds specs that drive asynchronous page state** (network
interception, fetch-then-render fixtures, anything with two DOM writes), have it also run `make
stress-test` itself before reporting done — this repo has caught races in newly-written specs
this way even when both normal lanes were green, and it's a cheap, precisely-targeted check of
exactly the axis that subagent touched.

Beyond that, **you own the broader flake hunts and never trust a subagent's "tests pass."**
Re-run suites yourself, and check the claim covers the *current* tree. Batch a handful of landed
changes and hunt after with `make stress-test`, or push a broader hunt to the end of the session
when the work is low-risk; hunt earlier and unbatched when the work touched shared test helpers,
fixtures, or anything timing-sensitive.

A golden-master gate (the committed visual-regression baselines in `biloba-baselines/`) needs a
different verification instruction than a test gate: "tests pass" is checkable, "the baseline is
correct" is not. A subagent that writes or updates a baseline via `BILOBA_UPDATE_SCREENSHOTS=1`
must READ each new or changed PNG and SAY IN WORDS what it shows you — you judge the description,
not the exit code. Skip this and you get baselines nobody ever looked at, which is an assertion
that cannot fail.

**When a hunt turns up a flake, reason from the evidence — do not try to reproduce it.** These
races are hard to repro and the recorded seed is rarely definitive, so re-running to "confirm" it
usually costs many minutes and comes back green, which proves nothing. Use `--json-report` and
inspect the failed spec's `CapturedGinkgoWriterOutput` and message, alongside the on-failure
artifacts Biloba itself produces (DOM outline, screenshots, the poll trajectory of the timed-out
read — see `biloba:debug-failures`). Form a hypothesis about the mechanism from those, and make a
reason-based fix with a written root cause. Re-stress afterwards — a fix can unmask a downstream
race. Send the flake back to the agent whose work introduced it, via `SendMessage`, with your
diagnosis pasted in — it still holds the context, and a fresh agent would only re-derive it; a
cross-cutting or pre-existing flake goes to a *different* agent. When you hand off a hypothesis,
give the subagent your mechanism guess *and* explicit permission to disagree with evidence — a
wrong hypothesis costs nothing when the reasoning behind it is exposed and open to refutation.

If a scope change lands mid-flight, send it to the agent that already owns that file via
`SendMessage` rather than spinning up a second writer for it — one writer at a time per file (and
especially per `biloba.js`/`dom.go`-style pair, since DOM interaction logic and its Go wrapper
move together).

## Never release

Onsi releases, using a `shipit` binary neither you nor any subagent may run. Confirm no subagent
bumped `BILOBA_VERSION`, edited `CHANGELOG.md` (the released log — `CHANGELOG-TMP.md` is where
staged notes belong), or tagged/published anything. Your job, and every subagent's, ends at
staging notes in `CHANGELOG-TMP.md`.

## Keep the skills in sync

Whenever a landed change adds or alters a method family, an option, a convention, or an env
knob, confirm the relevant skill was updated in the *same* change — both the repo skills under
`.claude/skills/` (which teach future work *on* Biloba) and, if the change is user-visible, the
shipped plugin skills under `plugins/biloba/skills/` (which teach users' agents how to *write
tests with* Biloba). Cross-references should stay consistent (a fact stated in one skill should
agree with the others that touch it).

**At the end of a session, ask the owner whether they'd like to perform a prompt audit.** Ask —
don't assume, and don't run one silently. The audit's goals are to keep the corpus (`CLAUDE.md`,
both skill surfaces above) free of **contradiction**, **unnecessary duplication**, **excessive
verbosity**, and **over-rotation onto incident-specific details where generalized guidance
belongs**. That last one is the failure mode that accumulates fastest: a rule written in the heat
of one incident tends to encode that incident's particulars — a file name, a spec, a specific
wrong turn — when what the next reader needs is the shape. Prefer the general statement, and keep
the incident only where it is the evidence that makes the rule credible.

Commit to master incrementally. Only use branches for riskier feature work or if the user
explicitly asks you to.
