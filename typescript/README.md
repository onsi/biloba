# Biloba for TypeScript

A TypeScript client for [Biloba](https://onsi.github.io/biloba/), so a `vitest` suite can drive the same browser automation Biloba brings to Ginkgo.

**This is a prototype.**  The package is not published to npm, and its API will shift more freely than the Go one does.

## The short version

Each `vitest` worker process spawns a small Go daemon (`bilobad`) and talks to it over framed JSON on stdin/stdout.  Every daemon attaches to one shared Chrome:

```
vitest worker 1  ──▶  bilobad  ──┐
vitest worker 2  ──▶  bilobad  ──┼──▶  one shared Chrome
vitest worker 3  ──▶  bilobad  ──┘
```

Polling happens on the daemon.  An assertion with a 1s timeout and a 5ms interval is *one* request, not two hundred - the retry loop runs next to Chrome and answers once with the outcome and the trajectory it took.

```ts
const browser = await connect({chromeWsUrl});
const session = await browser.openSession();

await session.navigate("http://localhost:8080");
await session.getByTestId("name").setValue("Ada");
await session.getByRole("button", {name: "Increment"}).click();
await session.locator("#count").expectText("1");
```

## Read this instead

The narrative documentation lives with the rest of Biloba's docs:
**[Biloba from TypeScript](https://onsi.github.io/biloba/#biloba-from-typescript)** — the topology and why it's shaped this way, setup, locators, actions and assertions, `evaluate`, failure output and what each error code means, and what the client doesn't cover yet.

## Working in this directory

The daemon has to be built first; the `make` targets in the repository root do that for you and point `BILOBA_DAEMON_EXECUTABLE` at it.

```bash
make driver-test     # TypeScript unit tests + the Go driver packages
make driver-parity   # the shared Go/TypeScript behavior contract, against a real Chrome
make driver-e2e      # the real topology: three worker processes, one bilobad each, one Chrome
```

Within this directory, `pnpm test`, `pnpm typecheck`, and `pnpm build` cover the unit-test loop.  `src/generated/protocol.ts` is generated from the Go protocol definition — run `go generate ./protocol` from the repository root after changing the wire structs, rather than editing it.
