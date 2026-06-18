# Biloba test targets. See the biloba-testing skill (.claude/skills/biloba-testing/SKILL.md) for
# details and guidance on when to reach for each.
#
# `go run` is used so no global ginkgo install is needed (matches how CI invokes it).

GINKGO := go run github.com/onsi/ginkgo/v2/ginkgo

.PHONY: test test-all stress-test update-chrome

## test: standard headless (chrome-headless-shell) suite - parallel + randomized. Your default.
test:
	$(GINKGO) -r -p --randomize-all

## test-all: both fidelity lanes CI runs - the default headless-shell lane, then the full ("new")
## headless google-chrome lane. Run before changes that touch tab/Chrome lifecycle.
test-all:
	$(GINKGO) -r -p --randomize-all
	BILOBA_TEST_HIGH_FIDELITY=true $(GINKGO) -r -p --randomize-all

## update-chrome: pull the latest stable chrome-headless-shell into the puppeteer cache Biloba
## searches first (~/.cache/puppeteer), so `make test` exercises the same Chrome CI auto-installs.
## Biloba reuses any cached binary rather than phoning home each run (offline-friendly + fast), so a
## stale local cache can hide a breakage that CI - which always tracks latest - catches. Run this
## periodically (or when CI goes red on a Chrome bump) to resync. The chrome-tracking workflow is
## the canonical "latest is green" signal.
update-chrome:
	npx -y @puppeteer/browsers install chrome-headless-shell@stable --path "$$HOME/.cache/puppeteer"

## stress-test: flake hunt - 6 procs under moderate CPU/IO load, 41 repeats, generous total budget
## so a wedge surfaces as a TIMEDOUT (with a goroutine dump) rather than a false budget-exhaustion.
## Slow; run periodically or when you suspect a change might be flaky. Needs `stress` (brew install stress).
## Cleanup reaps stress's worker children (pkill -P) before the launcher - SIGTERMing only the launcher
## orphans the --cpu/--io workers to init, where they peg the CPU until their own --timeout fires.
stress-test:
	@command -v stress >/dev/null || { echo "stress not found - install with: brew install stress"; exit 1; }
	stress --cpu 4 --io 1 --timeout 2000s & \
	stress_pid=$$!; \
	$(GINKGO) -procs=6 --randomize-all --repeat 40 --timeout=1500s --poll-progress-after=45s ./... ; \
	status=$$?; \
	pkill -P $$stress_pid 2>/dev/null; kill $$stress_pid 2>/dev/null; \
	exit $$status
