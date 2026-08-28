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
      if (request.url === "/download") { response.setHeader("content-disposition", "attachment; filename=eventful.bin"); response.end(Buffer.from([0xff, 0x00, 0x41])); return; }
      if (request.url === "/slow-download") { response.setHeader("content-disposition", "attachment; filename=slow.bin"); response.write(Buffer.alloc(1024)); const timer = setInterval(() => { if (response.destroyed) { clearInterval(timer); return; } response.write(Buffer.alloc(1024)); }, 50); setTimeout(() => { clearInterval(timer); response.end(); }, 1_000); return; }
      if (request.url === "/echo-request") { const chunks: Buffer[] = []; request.on("data", (chunk: Buffer) => chunks.push(chunk)); request.on("end", () => { response.setHeader("content-type", "application/json"); response.end(JSON.stringify({method: request.method, header: request.headers["x-modified"], body: Buffer.concat(chunks).toString("utf8")})); }); return; }
      if (request.url?.startsWith("/network-json") || request.url?.startsWith("/callback")) { response.setHeader("content-type", "text/plain"); response.setHeader("x-duplicate", ["first", "second"]); response.end(request.url.startsWith("/callback") ? "callback" : "network"); return; }
      if (request.url === "/slow") { setTimeout(() => response.end("slow"), 100); return; }
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

  it("bridges dialog, download, and network lifecycles through the real daemon", async () => {
    await session.prepare();
    await session.navigate(baseUrl);

    const dialogs = await session.handleDialogs("prompt", {message: contains("name"), accept: true, promptText: "Ada"});
    expect(await session.evaluate(`window.prompt("name", "default")`)).toBe("Ada");
    expect((await session.dialogs({type: "prompt"})).at(-1)).toMatchObject({accepted: true, promptText: "Ada", autoHandled: false});
    await dialogs.remove();
    expect(await session.evaluate(`window.confirm("default handling")`)).toBe(false);
    expect((await session.dialogs({type: "confirm"})).at(-1)).toMatchObject({accepted: false, autoHandled: true});

    await session.evaluate(`{ const a = document.createElement("a"); a.href = "/download"; a.download = "eventful.bin"; a.click(); }`);
    const download = await session.expectDownload({filename: "eventful.bin", state: "complete", content: new Uint8Array([0xff, 0x00, 0x41])}, {timeoutMs: 2_000});
    expect(await download.content({maxBytes: 16})).toEqual(new Uint8Array([0xff, 0x00, 0x41]));

    await session.evaluate(`{ const a = document.createElement("a"); a.href = "/slow-download"; a.download = "slow.bin"; a.click(); }`);
    const slowDownload = await session.expectDownload({filename: "slow.bin", state: "active"}, {timeoutMs: 2_000});
    await slowDownload.cancel();
    expect((await session.downloads({filename: "slow.bin"})).at(-1)?.state).toBe("cancelled");
    const stub = await session.stubRequest(endsWith("/stub"), {status: 201, headers: [{name: "x-stub", value: "yes"}], body: new TextEncoder().encode("stubbed")});
    const stubbedResponse = await session.evaluateAsync(`fetch("/stub").then(async response => [response.status, response.headers.get("x-stub"), await response.text()])`).catch((error: unknown) => { throw new Error("stubRequest live fetch failed", {cause: error}); });
    expect(stubbedResponse).toEqual([201, "yes", "stubbed"]);
    expect(await stub.count()).toBe(1);
    await stub.remove();

    const abort = await session.abortRequest(endsWith("/abort-me"));
    const abortedRequest = await session.evaluateAsync(`fetch("/abort-me")`).catch((error: unknown) => error);
    expect(abortedRequest, "abortRequest must fail the matching fetch").toBeInstanceOf(BilobaError);
    expect(await abort.count()).toBe(1);
    await abort.remove();

    const requestModifier = await session.modifyRequest(endsWith("/rewrite-me"), {
      url: `${baseUrl}/echo-request`,
      method: "POST",
      headers: [{name: "x-modified", value: "yes"}],
      body: new TextEncoder().encode("rewritten"),
    });
    const rewrittenRequest = await session.evaluateAsync(`fetch("/rewrite-me").then(response => response.json())`).catch((error: unknown) => { throw new Error("modifyRequest live fetch failed", {cause: error}); });
    expect(rewrittenRequest).toEqual({method: "POST", header: "yes", body: "rewritten"});
    await requestModifier.remove();

    const winner = await session.abortRequest(endsWith("/shadow"));
    const shadowed = await session.abortRequest(endsWith("/shadow"));
    expect(await session.evaluateAsync(`fetch("/shadow")`).catch((error: unknown) => error)).toBeInstanceOf(BilobaError);
    const shadow = (await session.networkShadowDiagnostics()).at(-1);
    expect(shadow?.winner.id).toBe(winner.id);
    expect(shadow?.shadowed.map(({id}) => id)).toContain(shadowed.id);
    await winner.remove();
    await shadowed.remove();

    const transform = await session.routeResponse(endsWith("/callback"), async (response) => ({status: 202, headers: [{name: "x-original-duplicates", value: String(response.headers.filter(({name}) => name.toLowerCase() === "x-duplicate").length)}], body: new TextEncoder().encode(new TextDecoder().decode(response.body).toUpperCase())}));
    expect(await session.evaluateAsync(`fetch("/callback").then(async response => [response.status, response.headers.get("x-original-duplicates"), await response.text()])`)).toEqual([202, "2", "CALLBACK"]);
    await transform.remove();

    const failingTransform = await session.routeResponse(endsWith("/callback"), () => { throw new Error("route failed"); });
    expect(await session.evaluateAsync(`fetch("/callback").then(response => response.text())`)).toBe("callback");
    expect((await failingTransform.stats()).lastError).toContain("route failed");
    await failingTransform.remove();
    const timedTransform = await session.routeResponse(endsWith("/callback"), async () => { await new Promise((resolve) => setTimeout(resolve, 100)); return {status: 204}; }, {timeoutMs: 20});
    expect(await session.evaluateAsync(`fetch("/callback").then(response => response.text())`)).toBe("callback");
    expect((await timedTransform.stats()).lastError).toBeTruthy();
    await timedTransform.remove();

    const responseModifier = await session.modifyResponse(endsWith("/network-json"), {status: 203, body: new TextEncoder().encode("modified")});
    expect(await session.evaluateAsync(`fetch("/network-json").then(async response => [response.status, await response.text()])`)).toEqual([203, "modified"]);
    await responseModifier.remove();

    const hold = await session.holdResponse(contains("/callback?hold="), {limit: 2, maxBodyBytes: 1_024});
    await session.evaluate(`{ void fetch("/callback?hold=one"); void fetch("/callback?hold=two"); void fetch("/callback?hold=three"); }`);
    const firstHeld = await hold.await({timeoutMs: 2_000});
    expect(firstHeld).toMatchObject({status: 200});
    expect(firstHeld.url).toContain("hold=");
    expect(new TextDecoder().decode(firstHeld.body)).toBe("callback");
    expect(firstHeld.headers.filter(({name}) => name.toLowerCase() === "x-duplicate")).toHaveLength(2);
    await expect.poll(async () => (await hold.stats()).holding, {timeout: 2_000}).toBe(2);
    await hold.release(firstHeld);
    const secondHeld = await hold.await({timeoutMs: 2_000});
    expect(secondHeld.id).not.toBe(firstHeld.id);
    await hold.releaseNext();
    const holdStats = await hold.stats();
    expect(holdStats.count).toBe(3);
    expect(holdStats.held + holdStats.passedThrough).toBe(3);
    expect(holdStats.holding).toBe(holdStats.held - 2);
    await hold.releaseAll();

    await session.evaluateAsync(`fetch("/observed", {method:"POST"}).then(response => response.text())`);
    expect((await session.waitForRequest({url: endsWith("/observed"), method: "POST"}, {timeoutMs: 1_000})).method).toBe("POST");
    expect((await session.requests({url: endsWith("/observed")})).length).toBeGreaterThan(0);
    expect((await session.responses({url: endsWith("/observed")})).length).toBeGreaterThan(0);
    expect((await session.expectNetworkIdle({timeoutMs: 1_000})).attemptCount).toBeGreaterThan(0);

    await session.setOffline(true);
    expect(await session.networkState()).toMatchObject({offline: true});
    expect(await session.evaluate("navigator.onLine")).toBe(false);
    await session.resetNetworkState();
    expect(await session.evaluateAsync(`fetch("/network-json").then(response => response.text())`)).toBe("network");
    await session.setCacheEnabled(false);
    await session.setCacheEnabled(true);
    let staleCallbackInvocations = 0;
    await session.routeResponse(endsWith("/callback"), () => { staleCallbackInvocations++; return {status: 204}; });
    await session.prepare();
    await session.navigate(baseUrl);
    expect(await session.evaluateAsync(`fetch("/callback").then(response => response.text())`)).toBe("callback");
    expect(staleCallbackInvocations).toBe(0);
  });

  it("pushes automatic dialog warnings to the warning sink and resets its cursor", async () => {
    const warnings: Array<{message: string}> = [];
    const warningBrowser = await connect({daemonExecutable: daemonExecutable!, chromeWsUrl: sharedBrowser.wsURL, artifactDir, warningSink: (warning) => warnings.push(warning)});
    try {
      const warningSession = await warningBrowser.openSession();
      await warningSession.navigate(baseUrl);
      await warningSession.evaluateAsync(`new Promise(resolve => setTimeout(() => { window.confirm("timer warning"); resolve(true); }, 25))`);
      expect(warnings.map(({message}) => message)).toContainEqual(expect.stringContaining("timer warning"));
      await warningSession.prepare();
      await warningSession.navigate(baseUrl);
      await warningSession.evaluateAsync(`new Promise(resolve => setTimeout(() => { window.alert("after prepare"); resolve(true); }, 25))`);
      expect(warnings.map(({message}) => message)).toContainEqual(expect.stringContaining("after prepare"));
    } finally {
      await warningBrowser.close();
    }
  });

  it("cancels active downloads explicitly and during prepare before close", async () => {
    const downloadBrowser = await connect({daemonExecutable: daemonExecutable!, chromeWsUrl: sharedBrowser.wsURL, artifactDir});
    try {
      const closeSession = await downloadBrowser.openSession();
      await closeSession.navigate(baseUrl);
      await closeSession.evaluate(`{ const a = document.createElement("a"); a.href = "/slow-download"; a.download = "slow.bin"; a.click(); }`);
      const closeDownload = await closeSession.expectDownload({filename: "slow.bin", state: "active"}, {timeoutMs: 2_000});
      await expect(closeSession.close()).rejects.toBeInstanceOf(BilobaError);
      expect(await closeSession.url()).toContain(baseUrl);
      await closeDownload.cancel();
      await closeSession.close();

      const prepareSession = await downloadBrowser.openSession();
      await prepareSession.navigate(baseUrl);
      await prepareSession.evaluate(`{ const a = document.createElement("a"); a.href = "/slow-download"; a.download = "slow.bin"; a.click(); }`);
      await prepareSession.expectDownload({filename: "slow.bin", state: "active"}, {timeoutMs: 2_000});
      await prepareSession.prepare();
      expect(await prepareSession.downloads()).toEqual([]);
      await prepareSession.close();
    } finally {
      await downloadBrowser.close();
    }
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

  it("keeps lifecycle state and live handles coherent through the real daemon", async () => {
    await session.prepare();
    const startingSize = await session.windowSize();
    await session.navigate(baseUrl);
    const startingEmulation = await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches, innerWidth, innerHeight, devicePixelRatio]`);
    const startingPermission = await session.evaluateAsync<string>(`navigator.permissions.query({name: "geolocation"}).then(result => result.state)`);
    expect(await session.url()).toBe(baseUrl + "/");
    await session.expectUrl(endsWith("/"));
    expect(await session.title()).toBe("");
    await session.evaluate(`document.title = "Biloba lifecycle"`);
    await session.expectTitle(contains("Biloba"));

    await session.setCookies([{name: "lifecycle", value: "ready", domain: "127.0.0.1", path: "/"}]);
    expect(await session.expectCookie({name: "lifecycle", value: "ready"})).toMatchObject({name: "lifecycle", value: "ready"});
    await session.expectCookieCount(numeric(">=", 1));
    expect(await session.findCookie({name: "lifecycle"})).toBeDefined();
    await session.clearCookies();
    expect(await session.getCookies()).toEqual([]);
    await session.setCookies([{name: "lifecycle", value: "ready", domain: "127.0.0.1", path: "/"}]);
    await session.localStorage().set("count", 3);
    await session.sessionStorage().set("token", {ready: true});
    expect(await session.localStorage().get<number>("count")).toEqual({found: true, value: 3});
    expect(await session.localStorage().getAll()).toEqual({count: 3});
    await session.localStorage().remove("count");
    expect(await session.localStorage().get("count")).toEqual({found: false});
    await session.localStorage().set("count", 3);
    expect(await session.sessionStorage().expectItem("token", {ready: true})).toEqual({ready: true});
    await session.sessionStorage().clear();
    expect(await session.sessionStorage().get("token")).toEqual({found: false});
    await session.sessionStorage().set("token", {ready: true});
    await session.localStorage().expectLength(1);
    await session.addInitScript(`window.lifecycleInit = "installed"`);

    await session.evaluate(`setTimeout(() => { window.lifecycleReady = 42 }, 25)`);
    expect(await session.waitForDefined<number>("window.lifecycleReady", {timeoutMs: 1_000})).toBe(42);
    const cancelled = new AbortController();
    const pendingWait = session.waitForDefined("window.neverDefined", {timeoutMs: 1_000, signal: cancelled.signal});
    cancelled.abort();
    await expect(pendingWait).rejects.toMatchObject({code: "CANCELLED"});
    expect(await session.outline()).toContain("Biloba parity");
    expect(await session.accessibilityOutline()).toContain("Biloba parity");
    await session.evaluate(`console.error("lifecycle-console")`);
    expect((await session.expectConsoleMessage("lifecycle-console", {type: "error"})).text).toBe("lifecycle-console");

    await session.setGeolocation({latitude: 0, longitude: 0, accuracy: 1});
    await session.setPermissions(baseUrl, {geolocation: "granted"});
    const position = await session.evaluateAsync<{latitude: number; longitude: number}>(`new Promise((resolve, reject) => navigator.geolocation.getCurrentPosition(p => resolve({latitude:p.coords.latitude, longitude:p.coords.longitude}), reject))`);
    expect(position).toEqual({latitude: 0, longitude: 0});
    await session.setLocale("fr-FR");
    await session.setTimezone("Europe/Paris");
    await session.setMedia({colorScheme: "dark", reducedMotion: "reduce"});
    await session.setDeviceMetrics({width: 320, height: 640, deviceScaleFactor: 2, mobile: true});
    expect(await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches]`)).toEqual(["fr-FR", "Europe/Paris", true]);
    expect(await session.evaluate(`[screen.width, screen.height, devicePixelRatio, innerWidth]`)).toEqual([320, 640, 2, 980]);
    await session.navigate(baseUrl);
    expect(await session.evaluate("window.lifecycleInit")).toBe("installed");

    await session.setWindowSize(640, 480);
    expect(await session.windowSize()).toEqual({width: 640, height: 480});
    const closedSibling = await session.newTab();
    await closedSibling.navigate(baseUrl);
    expect((await session.tabs()).map((tab) => tab.id)).toEqual(expect.arrayContaining([session.id, closedSibling.id]));
    await expect(closedSibling.prepare()).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await closedSibling.close();
    await expect(closedSibling.title()).rejects.toMatchObject({code: "DRIVER_CLOSED"});
    expect(await session.title()).toBe("");
    const sibling = await session.newTab();
    await sibling.navigate(baseUrl);
    await session.evaluate(`(() => { window.open(${JSON.stringify(baseUrl + "/?popup=1")}, "_blank"); return true })()`);
    const popup = await session.waitForTab({url: contains("popup=1")}, {timeoutMs: 2_000});
    expect(popup.contextId).toBe(session.contextId);
    expect((await session.spawnedTabs()).map((tab) => tab.id)).toContain(popup.id);
    expect((await session.findTab({url: contains("popup=1")}))?.id).toBe(popup.id);

    const prepared = await session.prepare();
    expect(prepared.invalidatedSessionIds).toEqual(expect.arrayContaining([sibling.id, popup.id]));
    await expect(sibling.title()).rejects.toMatchObject({code: "DRIVER_CLOSED"});
    expect(await session.windowSize()).toEqual(startingSize);
    expect(await session.consoleMessages()).toEqual([]);
    await session.navigate(baseUrl);
    expect(await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches, innerWidth, innerHeight, devicePixelRatio]`)).toEqual(startingEmulation);
    expect(await session.evaluateAsync<string>(`navigator.permissions.query({name: "geolocation"}).then(result => result.state)`)).toBe(startingPermission);
    await session.setPermissions(baseUrl, {geolocation: "granted"});
    const resetGeolocation = await session.evaluateAsync<number>(`new Promise(resolve => navigator.geolocation.getCurrentPosition(() => resolve(0), error => resolve(error.code), {timeout: 100}))`);
    expect(resetGeolocation).not.toBe(0);
    await session.resetPermissions();
    expect(await session.evaluate("typeof window.lifecycleInit")).toBe("undefined");
    expect(await session.localStorage().get("count")).toEqual({found: false});
    expect(await session.getCookies()).toEqual([]);
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
      const [firstDialogs, secondDialogs] = await Promise.all([
        session.handleDialogs("prompt", {accept: true, promptText: "first-worker"}),
        secondSession.handleDialogs("prompt", {accept: true, promptText: "second-worker"}),
      ]);
      const [firstValue, secondValue] = await Promise.all([
        session.evaluate<string>(`window.prompt("worker")`),
        secondSession.evaluate<string>(`window.prompt("worker")`),
      ]);
      expect([firstValue, secondValue]).toEqual(["first-worker", "second-worker"]);
      expect(await session.dialogs()).toHaveLength(1);
      expect(await secondSession.dialogs()).toHaveLength(1);

      await session.evaluate(`{ const a = document.createElement("a"); a.href = "/download"; a.download = "eventful.bin"; a.click(); }`);
      await session.expectDownload({filename: "eventful.bin", state: "complete"}, {timeoutMs: 2_000});
      expect(await secondSession.downloads()).toEqual([]);

      await session.prepare();
      expect(await secondSession.evaluate<string>(`window.prompt("still isolated")`)).toBe("second-worker");
      expect(await secondSession.dialogs()).toHaveLength(2);
      await secondDialogs.remove();
      await firstDialogs.remove().catch(() => undefined);
    } finally {
      await secondBrowser.close();
    }
  });
});
