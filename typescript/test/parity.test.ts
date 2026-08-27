import {createReadStream} from "node:fs";
import {mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {createServer, type Server} from "node:http";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import {afterAll, beforeAll, describe, expect, it} from "vitest";

import {
  allOf,
  BilobaError,
  connect,
  contains,
  empty,
  not,
  numeric,
  Keys,
  endsWith,
  startSharedBrowser,
  xpath,
  type Browser,
  type Session,
  type SharedBrowserProcess,
} from "../src/index.js";
import {expectTimedOutAction, expectTimedOutAssertion} from "./support/assertions.js";

const fixturePath = fileURLToPath(new URL("../../fixtures/graft-parity.html", import.meta.url));
const expectedPath = fileURLToPath(new URL("../../fixtures/graft-parity-expected.json", import.meta.url));
const daemonExecutable = process.env.BILOBA_DAEMON_EXECUTABLE;

// This is the only test that drives the real daemon against a real Chrome, so it fails loudly when
// it is not configured rather than skipping.  A suite that silently reports 4 skipped, exit 0 looks
// exactly like a suite that passed.  Opt out deliberately with BILOBA_SKIP_PARITY=true.
describe.skipIf(process.env.BILOBA_SKIP_PARITY === "true")("Go and TypeScript parity contract", () => {
  let server: Server;
  let baseUrl: string;
  let browser: Browser;
  let session: Session;
  let sharedBrowser: SharedBrowserProcess;
  let artifactDir: string;

  beforeAll(async () => {
    if (!daemonExecutable) {
      throw new Error(
        "BILOBA_DAEMON_EXECUTABLE is not set, so the Go/TypeScript parity contract cannot run.\n" +
        "Run `make driver-parity` from the repository root, or build the daemon with " +
        "`go build -o .bin/bilobad ./cmd/bilobad` and point BILOBA_DAEMON_EXECUTABLE at it.\n" +
        "Set BILOBA_SKIP_PARITY=true to skip this suite on purpose.",
      );
    }
    server = createServer((request, response) => {
      response.setHeader("content-type", "text/html");
      // A 4xx that still renders HTML - the case that makes navigate()'s 200 assertion something you
      // need a way out of, rather than a rule that is always right.
      if (request.url === "/not-found") response.statusCode = 404;
      createReadStream(fixturePath).pipe(response);
    });
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("fixture server did not bind TCP");
    baseUrl = `http://127.0.0.1:${address.port}`;
    artifactDir = await mkdtemp(join(tmpdir(), "biloba-parity-"));
    // No chromePath: bilobad runs the same runner-neutral Chrome search the Go suite does, so this
    // exercises the resolution path a real worker takes.
    sharedBrowser = await startSharedBrowser({executable: daemonExecutable});
    browser = await connect({daemonExecutable: daemonExecutable, chromeWsUrl: sharedBrowser.wsURL, artifactDir});
    session = await browser.openSession();
  });

  afterAll(async () => {
    await browser?.close();
    await sharedBrowser?.stop();
    if (server) {
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
    if (artifactDir) await rm(artifactDir, {recursive: true, force: true});
  });

  it("reaches the same shared observable outcome through the TypeScript API", async () => {
    await session.prepare();
    await session.navigate(baseUrl);
    await session.getByRole("heading", {name: "Biloba parity"}).expectVisible();
    const delayed = await session.locator("#delayed").expectText("ready", {timeoutMs: 1_000, intervalMs: 5});
    expect(delayed.attemptCount).toBeGreaterThan(1);

    await session.getByTestId("name").setValue("Ada");
    await session.getByTestId("name").expectValue("Ada");
    await session.getByRole("button", {name: "Increment"}).click();
    await session.xpath(xpath("button").withText("Increment")).expectVisible();
    await session.locator(".row")
      .filter({hasText: "Ada"})
      .within("#results")
      .nth(1)
      .expectText("Ada second");
    await session.getByTestId("sent").or(session.getByTestId("delivered")).last().expectText("Delivered");
    await session.getByRole("button", {name: "Increment"}).expectEnabled();
    await session.getByRole("button", {name: "Increment"}).expectClickable();
    await session.getByTestId("missing").expectNotExists();
    await session.locator(".row").expectCount(numeric(">=", 3));
    const stable = await session.locator("#delayed").expectProperty(
      "textContent",
      allOf(contains("ready"), not(empty())),
      {mode: "consistently", timeoutMs: 30, intervalMs: 5},
    );
    expect(stable.attemptCount).toBeGreaterThan(1);
    expect(await session.locator(".row").count()).toBe(3);
    expect(await session.locator("#delayed").text()).toBe("ready");
    expect(await session.getByTestId("name").value()).toBe("Ada");
    expect(await session.url()).toBe(baseUrl + "/");

    const actual = await session.evaluate(`({
      count: document.querySelector('#count').innerText,
      delayed: document.querySelector('#delayed').innerText,
      echo: document.querySelector('#echo').innerText,
      heading: document.querySelector('h1').innerText,
      value: document.querySelector('[data-testid="name"]').value,
    })`);
    const expected = JSON.parse(await readFile(expectedPath, "utf8")) as unknown;
    expect(actual).toEqual(expected);

    const realisticName = session.getByTestId("name").realistic();
    await realisticName.setValue("Grace");
    await realisticName.type(" Hopper");
    await session.getByRole("button", {name: "Increment"}).realistic().click();
    await session.sendKeys(Keys.Escape);
    await session.expectEvaluation(
      `document.querySelector('[data-testid="name"]').value + '|' + document.querySelector('#count').textContent + '|' + document.querySelector('#last-key').textContent`,
      "Grace Hopper|2|Escape",
    );

    // An args array - empty or not - means "call this function"; no args array means "read this
    // expression".  The daemon is told which, so neither reading depends on the argument count.
    expect(await session.evaluate<number>("() => 41 + 1", [])).toBe(42);
    expect(await session.evaluate<number>("(a, b) => a + b", [40, 2])).toBe(42);

    expect(await session.evaluateAsync<string>(`Promise.resolve("settled")`)).toBe("settled");
    await session.setWindowSize(375, 812);
    expect(await session.evaluate(`[window.innerWidth, window.innerHeight]`)).toEqual([375, 812]);
    const uploadPath = join(artifactDir, "avatar.txt");
    await writeFile(uploadPath, "avatar");
    await session.getByTestId("upload").setUploadFiles([uploadPath]);
    expect(await session.evaluate(`document.querySelector('[data-testid="upload"]').files[0].name`)).toBe("avatar.txt");
    await session.evaluateAsync(`fetch("/observed", {method:"POST"}).then(response => response.text())`);
    await session.expectRequest(endsWith("/observed"), {method: "POST"});
    const hold = await session.holdResponse(endsWith("/held"));
    await session.getByTestId("held-request").click();
    expect((await hold.await()).status).toBe(200);
    await hold.release();
    await session.setWindowSize(1920, 1080);
  });

  it("treats a non-200 page as navigable when the caller asks for that status", async () => {
    // Pairs with "treats a non-200 page as navigable when the spec asks for that status" in
    // graft_parity_test.go.  Go has Navigate/NavigateWithStatus; if TypeScript has only the former,
    // an error page is testable from one API and unreachable from the other.
    await session.prepare();
    await session.navigateWithStatus(`${baseUrl}/not-found`, 404);
    await session.getByRole("heading", {name: "Biloba parity"}).expectVisible();

    const failure = await session.navigate(`${baseUrl}/not-found`).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(BilobaError);
    expect((failure as BilobaError).code, "a wrong status is not a driver fault and not a retryable one").toBe("NAVIGATION");
    expect((failure as BilobaError).message).toContain("expected HTTP status 200");
  });

  it("returns structured failure output from the real daemon", async () => {
    await session.prepare();
    await session.navigate(baseUrl);

    const failure = await session.locator("#never").expectText("ready", {timeoutMs: 40, intervalMs: 5})
      .catch((error: unknown) => error);

    // Same contract the stub daemon is held to in client.test.ts - if the stub ever drifts from
    // what a real bilobad does here, this line fails.
    expectTimedOutAssertion(failure, {locator: `locator("#never")`, expected: "ready"});
    expect(failure.trajectory.length).toBeGreaterThan(1);
    expect(failure.domOutline).toContain("Biloba parity");
    expect(failure.screenshotPath).toMatch(/biloba-failure-\d+\.png$/);
  });

  it("rejects an action that exhausts its server-side poll", async () => {
    await session.prepare();
    await session.navigate(baseUrl);

    const failure = await session.locator("#never").click({timeoutMs: 40, intervalMs: 5})
      .catch((error: unknown) => error);

    expectTimedOutAction(failure, {locator: `locator("#never")`, operation: "click"});
    expect(failure.trajectory.length).toBeGreaterThan(1);
    expect(failure.domOutline).toContain("Biloba parity");
  });

  it("runs independent worker daemons concurrently against the shared Chrome", async () => {
    const secondBrowser = await connect({daemonExecutable: daemonExecutable!, chromeWsUrl: sharedBrowser.wsURL});
    const secondSession = await secondBrowser.openSession();
    try {
      await Promise.all([session.prepare(), secondSession.prepare()]);
      await Promise.all([session.navigate(baseUrl), secondSession.navigate(baseUrl)]);
      const [firstValue, secondValue] = await Promise.all([
        session.evaluate<number>("1 + 1"),
        secondSession.evaluate<number>("2 + 2"),
      ]);
      expect([firstValue, secondValue]).toEqual([2, 4]);
    } finally {
      await secondBrowser.close();
    }
  });
});
