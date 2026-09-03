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
  equalTo,
  not,
  numeric,
  optionLabel,
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
    await session.getByRole("heading", {name: "Biloba parity"}).level(1).expectVisible();
    const delayed = await session.locator("#delayed").expectText("ready", {timeoutMs: 1_000, intervalMs: 5});
    expect(delayed.attemptCount).toBeGreaterThan(1);

    await session.getByLabel("Name", {exact: true}).setValue("Ada");
    await session.getByLabel("Name", {exact: true}).expectValue("Ada");
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

  it("keeps the typed DOM surface runner-neutral through the real daemon", async () => {
    await session.prepare();
    await session.navigate(baseUrl);

    await session.getByLabel("Name", {exact: true}).within("#profile").expectVisible();
    await session.getByPlaceholder("Full name").expectVisible();
    await session.getByAltText("Parity mark").expectVisible();
    await session.getByTitle("Biloba image").expectVisible();
    await session.getByTestId("custom-id", {attribute: "data-qa"}).expectText("custom selector");
    await session.xpath(xpath("div").withID("surface")).and(".primary").expectVisible();

    const surface = session.locator("#surface");
    expect(await surface.innerText()).toContain("DOM");
    expect(await surface.textContent()).toContain("surface");
    expect(await surface.normalizedText()).toBe("DOM surface");
    await surface.expectNormalizedText("DOM surface");
    // Go's HaveText normalizes innerText - the rendered text.  #rendered-text is the fixture that can
    // tell the two sources apart: normalizing textContent instead would return the display:none span
    // and would miss the text-transform.
    const rendered = session.locator("#rendered-text");
    expect(await rendered.normalizedText()).toBe("visible SHOUT");
    await rendered.expectNormalizedText("visible SHOUT");
    expect(await rendered.textContent()).toContain("SECRET");
    expect(await session.locator("#rendered-text").currentNormalizedTexts()).toEqual(["visible SHOUT"]);
    expect(await surface.innerHTML()).toContain("DOM");
    await surface.expectInnerText(contains("DOM"));
    await surface.expectTextContent(contains("surface"));
    await surface.expectInnerHTML(contains("DOM"));
    expect(await session.locator(".surface-item").currentInnerTexts()).toEqual(["first", "second"]);
    expect(await session.locator(".surface-item").currentTextContents()).toEqual(["first", "second"]);
    expect(await session.locator(".surface-item").currentNormalizedTexts()).toEqual(["first", "second"]);
    await session.locator(".surface-item").expectEachInnerText(/first|second/);
    await session.locator(".surface-item").expectEachTextContent(/first|second/);
    await session.locator(".surface-item").expectEachNormalizedText(/first|second/);
    expect(await surface.classes()).toEqual(["primary", "wide"]);
    expect(await session.locator(".surface-item").currentClasses()).toEqual([["surface-item"], ["surface-item"]]);
    await surface.expectClass("primary");
    await session.locator(".surface-item").expectEachClass("surface-item");
    await session.locator(".surface-item").expectDistinctAttributeCount("data-kind", 2);
    expect(await surface.attributes(["class", {name: "missing", allowMissing: true}])).toEqual({class: "primary wide", missing: null});
    await surface.expectAttributePresent("class");
    expect(await session.locator(".surface-item").currentAttributes(["data-kind"])).toEqual([{ "data-kind": "one" }, { "data-kind": "two" }]);
    await session.locator(".surface-item").expectEachAttribute("data-kind", /one|two/);
    expect(await surface.jsonAttribute("data-json")).toEqual({ready: true});
    await surface.expectJSONAttribute("data-json", {ready: true});
    expect(await surface.properties(["id", {name: "missing", allowMissing: true}])).toEqual({id: "surface", missing: null});
    await surface.expectPropertyPresent("id");
    expect(await session.locator(".surface-item").currentProperty("textContent")).toEqual(["first", "second"]);
    expect(await session.locator(".surface-item").currentProperties(["id"])).toEqual([{id: ""}, {id: ""}]);
    await session.locator(".surface-item").expectEachProperty("className", "surface-item");

    const name = session.getByTestId("name");
    await name.setValue("Ada");
    expect(await name.currentValues()).toEqual(["Ada"]);
    await name.focus();
    await name.expectFocused();
    await name.blur();
    await name.expectNotFocused();
    await name.type("a", {modifiers: ["Shift"]});
    expect(await name.value()).toBe("Adaa");
    await session.sendKeys("x", {modifiers: ["Control"]});
    expect(await session.locator("#key-modifiers").textContent()).toBe("x:false:true");
    await session.locator("#accepted").setValue(true);
    await session.locator("#accepted").expectChecked();
    await session.locator("#choice").selectOption(optionLabel("Beta"));
    expect(await session.locator("#choice").value()).toBe("b");

    await surface.setProperty("dataset.state", "changed");
    await session.locator(".surface-item").setPropertyAll("dataset.changed", true);
    expect(await session.locator(".surface-item").currentProperty("dataset.changed")).toEqual(["true", "true"]);
    expect(await surface.getAttribute("data-state")).toBe("changed");
    expect(await surface.invokeMethod("getAttribute", ["data-state"])).toBe("changed");
    expect(await surface.invoke("(element, prefix) => prefix + element.id", ["id:"])).toBe("id:surface");
    expect(await session.locator(".surface-item").invokeMethodAll("getAttribute", ["data-kind"])).toEqual(["one", "two"]);
    expect(await session.locator(".surface-item").invokeAll("element => element.textContent.toUpperCase()")).toEqual(["FIRST", "SECOND"]);

    await session.locator("#pointer-target").click({position: {x: 3, y: 3}, modifiers: ["Shift"]});
    expect(await session.locator("#pointer-log").textContent()).toBe("click:0:true");
    await session.locator("#pointer-target").dblclick();
    expect(await session.locator("#pointer-log").textContent()).toBe("double");
    await session.locator("#pointer-target").rightClick();
    expect(await session.locator("#pointer-log").textContent()).toBe("right");
    await session.locator("#pointer-target").middleClick();
    expect(await session.locator("#pointer-log").textContent()).toBe("middle");
    await session.locator(".bulk").clickAll();
    expect(await session.locator("#bulk-count").textContent()).toBe("2");
    await session.locator("#pointer-target").tap();
    await session.locator("#pointer-target").realistic().tap();
    await session.locator("#hover-target").hover();
    await session.locator("#pointer-target").realistic().click();
    await session.locator("#hover-target").realistic().hover();
    await session.locator(".bulk").realistic().clickAll();
    expect(await session.locator("#bulk-count").textContent()).toBe("4");
    await session.locator("#drag-source").dragTo("#drag-target");
    await session.locator("#drag-source").realistic().dragTo("#drag-target");
    expect(await session.locator("#drag-count").textContent()).toBe("2");

    const scrollbox = session.locator("#scrollbox");
    await session.locator("#scroll-target").scrollIntoView({within: scrollbox, topOffset: 5});
    expect((await scrollbox.scrollOffset()).top).toBeGreaterThan(0);
    await scrollbox.scrollWheel(0, 10);
    await scrollbox.realistic().scrollWheel(0, -10);
    await session.locator("#selection").selectText({substring: "phrase", occurrence: 2});
    expect(await session.evaluate("window.getSelection().toString()")).toBe("phrase");
    await session.locator("#selection").selectRange(0, 6);
    expect(await session.evaluate("window.getSelection().toString()")).toBe("select");
    await session.clearSelection();
    expect(await session.evaluate("window.getSelection().toString()")).toBe("");

    const box = await session.locator("#geometry-a").boundingBox();
    expect(box.width).toBe(60);
    expect((await session.locator("#geometry-a").offsetWithin("#geometry")).top).toBe(10);
    expect((await session.locator("#geometry-a").relativeBoxes("#geometry-b")).other.top).toBeGreaterThan(box.top);
    expect((await session.locator("#geometry-a").gapBetween("#geometry-b")).top).toBe(-60);
    expect(await session.locator("#geometry-a").documentOrder("#geometry-b")).toBe("before");
    // Every one of these is fed the exact value its own read just returned.  The daemon answers in Go
    // types (a named string for document order, structs for the boxes) while the expectation arrives
    // as decoded JSON, so without rendering the observation into its JSON shape these assertions can
    // never pass - they can only time out, which is indistinguishable from a slow page.
    await session.locator("#geometry-a").expectDocumentOrder("#geometry-b", "before");
    await session.locator("#geometry-a").expectBefore("#geometry-b");
    await session.locator("#geometry-b").expectAfter("#geometry-a");
    await session.locator("#geometry-a").expectBoundingBox(equalTo(box));
    await session.locator("#geometry-a").expectScrollOffset(equalTo(await session.locator("#geometry-a").scrollOffset()));
    await session.locator("#geometry-a").expectOffsetWithin("#geometry", equalTo(await session.locator("#geometry-a").offsetWithin("#geometry")));
    await session.locator("#geometry-a").expectRelativeBoxes("#geometry-b", equalTo(await session.locator("#geometry-a").relativeBoxes("#geometry-b")));
    await session.locator("#geometry-a").expectGapBetween("#geometry-b", equalTo(await session.locator("#geometry-a").gapBetween("#geometry-b")));
    await session.locator("#geometry-a").expectGeometry("above", "#geometry-b");
    await session.locator("#geometry-a").expectInViewport({fully: true});
    expect(await surface.computedStyle("color")).toBe("rgb(10, 20, 30)");
    expect(await surface.computedStyleNumber("font-size")).toBe(16);
    await surface.expectComputedStyle("color", "rgb(10, 20, 30)");
    await surface.expectComputedStyleNumber("font-size", 16);
    await surface.expectComputedColor("color", "#0a141e");
    expect(await session.normalizeColor("#0a141e")).toBe("rgb(10, 20, 30)");
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

  it("refuses to satisfy an assertion whose selector never matched", async () => {
    // biloba.js raises a missing element as an error rather than answering false, so that a negated
    // matcher cannot pass against a selector that matches nothing - a vacuous green test.  The same
    // rule is what makes a read poll: expectNot* here must time out, not return "no".
    await session.prepare();
    await session.navigate(baseUrl);
    const fast = {timeoutMs: 60, intervalMs: 5};

    await expect(session.locator("#never").expectNotVisible(fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectNotEnabled(fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectNotClickable(fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectNotChecked(fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectNotFocused(fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectText(empty(), fast)).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#never").expectValue(empty(), fast)).rejects.toMatchObject({code: "TIMEOUT"});

    // An element that exists but cannot answer the question is the same footgun: coercing the absent
    // property to false would make expectNotChecked pass forever against something never checkable.
    await expect(session.locator("#profile label").expectNotChecked(fast)).rejects.toMatchObject({code: "TIMEOUT"});

    // #accepted is a real checkbox, so the negation is answerable and must resolve.
    await session.locator("#accepted").expectNotChecked(fast);
    // Snapshot reads still answer "nothing matched" rather than failing.
    await session.locator("#never").expectNotExists(fast);
    expect(await session.locator("#never").count()).toBe(0);
  });

  it("polls a value read until the element arrives, rather than answering null", async () => {
    await session.prepare();
    await session.navigate(baseUrl);
    await session.evaluate(`setTimeout(() => {
      const input = document.createElement("input"); input.id = "late-value"; input.value = "arrived";
      const div = document.createElement("div"); div.id = "late-text"; div.textContent = "late text";
      document.body.append(input, div);
    }, 250)`);

    expect(await session.locator("#late-value").value({timeoutMs: 5_000})).toBe("arrived");
    expect(await session.locator("#late-text").text({timeoutMs: 5_000})).toBe("late text");
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
