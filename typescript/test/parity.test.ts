import {createReadStream} from "node:fs";
import {mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {createServer, type Server} from "node:http";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import {afterAll, beforeAll, beforeEach, describe, expect, it} from "vitest";

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
// The Go suite's own fixtures, so the pointer, screencast, and clock specs here drive the pages Go's
// pointer_test.go, screencast_test.go, and clock_test.go do.
const goFixture = (name: string) => fileURLToPath(new URL(`../../fixtures/${name}`, import.meta.url));

// This is the only test that drives the real daemon against a real Chrome, so it fails loudly when
// it is not configured rather than skipping.  A suite that silently reports 4 skipped, exit 0 looks
// exactly like a suite that passed.  Opt out deliberately with BILOBA_SKIP_PARITY=true.
describe.skipIf(process.env.BILOBA_SKIP_PARITY === "true")("Go and TypeScript parity contract", () => {
  let server: Server;
  let childServer: Server;
  let baseUrl: string;
  let childBaseUrl: string;
  let browser: Browser;
  let session: Session;
  let sharedBrowser: SharedBrowserProcess;
  let artifactDir: string;
  let baselineDir: string;
  const screenshotWarnings: string[] = [];
  let pointerReleases = 0;

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
      if (request.url === "/pointer.html" || request.url === "/screencast.html" || request.url === "/clock.html") { response.setHeader("content-type", "text/html"); createReadStream(goFixture(request.url.slice(1))).pipe(response); return; }
      if (request.url === "/released") { pointerReleases++; response.end(); return; }
      if (request.url === "/user-agent") { response.setHeader("content-type", "text/plain"); response.end(request.headers["user-agent"] ?? ""); return; }
      if (request.url === "/empty") { response.statusCode = 202; response.end(); return; }
      if (request.url === "/slow") { setTimeout(() => response.end("slow"), 100); return; }
      if (request.url === "/slow-callback") {
        response.setHeader("content-type", "text/plain");
        response.flushHeaders();
        setTimeout(() => response.end("slow body"), 600);
        return;
      }
      if (request.url === "/redirect-response") {
        response.statusCode = 302;
        response.setHeader("location", "/redirect-final");
        response.end();
        return;
      }
      if (request.url === "/redirect-final") { response.end("redirect final"); return; }
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

    childServer = createServer((request, response) => {
      if (request.url === "/pointer.html" || request.url === "/clock.html") { response.setHeader("content-type", "text/html"); createReadStream(goFixture(request.url.slice(1))).pipe(response); return; }
      const id = new URL(request.url ?? "/", "http://child.test").searchParams.get("id") ?? "child";
      const serializedId = JSON.stringify(id);
      response.setHeader("content-type", "text/html");
      response.end(`<!doctype html><title>${id}</title><script>window.frameState = {id: ${serializedId}, count: 1}; let frameLexical = "lexical-" + ${serializedId};</script><form onsubmit="event.preventDefault(); success.hidden=false; success.textContent=document.querySelector('input').value"><label>Email <input name="email"></label><button type="submit">Submit</button></form><div id="success" hidden></div><input id="frame-upload" type="file">`);
    });
    await new Promise<void>((resolve, reject) => {
      childServer.once("error", reject);
      childServer.listen(0, "127.0.0.1", resolve);
    });
    const childAddress = childServer.address();
    if (!childAddress || typeof childAddress === "string") throw new Error("child fixture server did not bind TCP");
    childBaseUrl = `http://127.0.0.1:${childAddress.port}`;
    artifactDir = await mkdtemp(join(tmpdir(), "biloba-parity-"));
    baselineDir = join(artifactDir, "baselines");
    // No chromePath: bilobad runs the same runner-neutral Chrome search the Go suite does, so this
    // exercises the resolution path a real worker takes.
    // BILOBA_TEST_HIGH_FIDELITY=true runs the contract against full headless Chrome, the Go suite's second lane.
    sharedBrowser = await startSharedBrowser({executable: daemonExecutable, ...(process.env.BILOBA_TEST_HIGH_FIDELITY === "true" && {mode: "headless" as const})});
    browser = await connect({daemonExecutable: daemonExecutable, chromeConnection: sharedBrowser.connection, artifactDir, screenshotBaselinesDir: baselineDir, onScreenshotWarning: ({message}) => screenshotWarnings.push(message)});
    session = await browser.openSession();
  });

  it("drives same-site cross-origin frames through the TypeScript client", async () => {
    expect(browser.launch).toMatchObject({attached: true, source: "shared-host", chromeArgs: [], windowSize: {width: 1024, height: 768}});
    const frameOwner = await browser.openSession();
    expect(frameOwner.contextId).not.toBe(session.contextId);
    await frameOwner.navigate(baseUrl);
    await frameOwner.evaluate(`url => {
      document.body.innerHTML = '<button id="parent-trap" style="position:fixed;left:0;top:0;width:250px;height:150px">Parent</button><input id="parent-upload" type="file">';
      document.querySelector("#parent-trap").onclick = event => { event.currentTarget.dataset.clicked = "yes"; };
      for (const [index, id] of ["one", "two"].entries()) {
        const frame = document.createElement("iframe");
        frame.id = id;
        frame.style.cssText = "position:fixed;left:400px;top:" + (100 + index * 300) + "px;width:320px;height:220px;border:8px solid black;transform:rotate(1deg);transform-origin:top left";
        frame.src = url + "/child-form?id=" + id;
        document.body.append(frame);
      }
    }`, [childBaseUrl]);

    const frame = await frameOwner.waitForFrame({url: {kind: "contains", expected: "id=one"}, has: 'input[name="email"]'}, {timeoutMs: 5_000});
    const second = await frameOwner.waitForFrame({title: "two"}, {timeoutMs: 5_000});
    expect(frame).toMatchObject({isFrame: true, frameUrl: expect.stringContaining("id=one"), frameId: expect.any(String)});
    expect(second.frameId).not.toBe(frame.frameId);
    expect(await frameOwner.frames()).toHaveLength(2);

    await frameOwner.evaluate(`window.frameState = {id: "parent", count: 10}; window.frameLexical = "parent"`);
    expect(await frame.evaluate(`[window.frameState.id, window.frameState.count, frameLexical]`)).toEqual(["one", 1, "lexical-one"]);
    await frame.evaluate(`window.frameState.count = 2; frameLexical = "changed-in-one"`);
    expect(await frame.evaluate(`[window.frameState.count, frameLexical]`)).toEqual([2, "changed-in-one"]);
    expect(await second.evaluate(`[window.frameState.id, window.frameState.count, frameLexical]`)).toEqual(["two", 1, "lexical-two"]);
    expect(await frameOwner.evaluate(`[window.frameState.id, window.frameState.count, window.frameLexical]`)).toEqual(["parent", 10, "parent"]);

    await frame.locator('input[name="email"]').setValue("ada@example.com");
    await frame.locator('button[type="submit"]').realistic().click();
    await frame.locator("#success").expectVisible();
    await frame.locator("#success").expectText("ada@example.com");
    expect(await frameOwner.evaluate(`document.querySelector('#parent-trap').dataset.clicked || ''`)).toBe("");
    const frameUpload = join(artifactDir, "frame-upload.txt");
    await writeFile(frameUpload, "frame");
    await frame.locator("#frame-upload").setUploadFiles([frameUpload]);
    expect(await frame.evaluate(`document.querySelector('#frame-upload').files[0].name`)).toBe("frame-upload.txt");
    expect(await frameOwner.evaluate(`document.querySelector('#parent-upload').files.length`)).toBe(0);
    await second.locator('input[name="email"]').setValue("grace@example.com");
    await frame.locator('input[name="email"]').expectValue("ada@example.com");

    // A same-process frame shares its tab's renderer attachment: tab controls must refuse rather than
    // act on the parent page, and the handle must still see its own document's console output.
    await expect(frame.navigate(`${childBaseUrl}/child-form?id=elsewhere`)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await expect(frame.addInitScript("window.fromFrameInitScript = true")).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await expect(frame.setOffline(true)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await expect(frame.setUserAgent("BilobaFrame/1.0")).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await expect(frame.setDeviceMetrics({width: 400, height: 300, touch: true})).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    await frameOwner.expectUrl(`${baseUrl}/`, {exact: true});
    await frame.evaluate(`console.log("frame-console-" + window.frameState.id)`);
    expect((await frame.expectConsoleMessage("frame-console-one")).text).toBe("frame-console-one");

    await frameOwner.evaluate(`document.querySelector("#one").remove()`);
    await expect(frame.locator("#success").expectVisible()).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    await expect(frame.evaluate(`window.frameState.id`)).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});

    await frameOwner.evaluate(`url => { document.querySelector("#two").src = url + "/child-form?id=replaced"; }`, [childBaseUrl]);
    const replacement = await frameOwner.waitForFrame({url: {kind: "contains", expected: "id=replaced"}}, {timeoutMs: 5_000});
    await expect(second.locator('input[name="email"]').expectVisible()).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    await expect(second.evaluate(`window.frameState.id`)).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    expect(await replacement.evaluate(`[window.frameState.id, frameLexical]`)).toEqual(["replaced", "lexical-replaced"]);

    await frameOwner.navigate(baseUrl);
    await expect(replacement.locator('input[name="email"]').expectVisible()).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    await expect(replacement.evaluate(`window.frameState.id`)).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    await expect(replacement.waitForRequest({url: contains("/never")}, {timeoutMs: 100})).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
    await expect(replacement.expectNetworkIdle({timeoutMs: 100})).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});

    await frameOwner.evaluate(`url => { const frame = document.createElement("iframe"); frame.src = url + "/child-form?id=prepare"; document.body.append(frame); }`, [childBaseUrl]);
    const preparedFrame = await frameOwner.waitForFrame({title: "prepare"}, {timeoutMs: 5_000});
    await frameOwner.prepare();
    await expect(preparedFrame.title()).rejects.toMatchObject({code: "DRIVER_CLOSED"});
    await frameOwner.close();
    expect(await session.title()).toBe("");
  });

  it("drives an out-of-process iframe through its own renderer target", async () => {
    const isolatedChrome = await startSharedBrowser({executable: daemonExecutable!, chromeArgs: ["--site-per-process"]});
    let isolatedBrowser: Browser | undefined;
    try {
      isolatedBrowser = await connect({daemonExecutable: daemonExecutable!, chromeConnection: isolatedChrome.connection, artifactDir});
      expect(isolatedBrowser.launch.chromeArgs).toContain("--site-per-process");
      const owner = await isolatedBrowser.openSession();
      const otherTab = await owner.newTab();
      await Promise.all([owner.navigate(baseUrl), otherTab.navigate(baseUrl)]);
      const oopifBaseUrl = childBaseUrl.replace("127.0.0.1", "localhost");
      await owner.evaluate(`url => {
        window.frameState = {id: "parent", count: 10};
        document.body.innerHTML = '<input id="parent-upload" type="file"><iframe id="oopif" style="width:360px;height:240px" src="' + url + '/child-form?id=oopif"></iframe><iframe id="peer" style="width:360px;height:240px" src="' + url + '/child-form?id=peer"></iframe>';
      }`, [oopifBaseUrl]);

      const frame = await owner.waitForFrame({title: "oopif", has: 'input[name="email"]'}, {timeoutMs: 5_000});
      const peer = await owner.waitForFrame({title: "peer"}, {timeoutMs: 5_000});
      expect(frame).toMatchObject({isFrame: true, frameUrl: `${oopifBaseUrl}/child-form?id=oopif`});
      expect(frame.targetId).not.toBe(owner.targetId);
      expect(frame.targetId).not.toBe("");
      expect(await frame.evaluate(`[window.frameState.id, window.frameState.count, frameLexical]`)).toEqual(["oopif", 1, "lexical-oopif"]);
      expect(await owner.evaluate(`window.frameState`)).toEqual({id: "parent", count: 10});

      await frame.locator('input[name="email"]').realistic().setValue("oopif@example.com");
      await frame.locator('button[type="submit"]').realistic().click();
      await frame.locator("#success").expectText("oopif@example.com");
      const upload = join(artifactDir, "oopif-upload.txt");
      await writeFile(upload, "oopif");
      await frame.locator("#frame-upload").setUploadFiles([upload]);
      expect(await frame.evaluate(`document.querySelector("#frame-upload").files[0].name`)).toBe("oopif-upload.txt");
      expect(await peer.locator('input[name="email"]').value()).toBe("");
      expect(await owner.evaluate(`document.querySelector("#parent-upload").files.length`)).toBe(0);
      expect(await otherTab.getByTestId("upload").value()).toBe("");

      await expect(frame.navigate(`${oopifBaseUrl}/child-form?id=elsewhere`)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await expect(frame.addInitScript("window.fromFrameInitScript = true")).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await expect(frame.setOffline(true)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await expect(frame.setUserAgent("BilobaFrame/1.0")).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await expect(frame.captureScreenshot()).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("capture the iframe element")});
      expect((await owner.locator("#oopif").captureScreenshot()).byteLength).toBeGreaterThan(0);

      await frame.close();
      expect(await owner.evaluate(`document.querySelector("#oopif").isConnected`)).toBe(true);
      expect(await peer.evaluate(`window.frameState.id`)).toBe("peer");
      expect(await otherTab.getByRole("heading", {name: "Biloba parity"}).text()).toBe("Biloba parity");

      const reopened = await owner.waitForFrame({title: "oopif"}, {timeoutMs: 5_000});
      await owner.evaluate(`url => { document.querySelector("#oopif").src = url + "/child-form?id=replaced"; }`, [oopifBaseUrl]);
      const replacement = await owner.waitForFrame({title: "replaced"}, {timeoutMs: 5_000});
      await expect(reopened.evaluate(`window.frameState.id`)).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
      expect(await replacement.evaluate(`window.frameState.id`)).toBe("replaced");
      await owner.navigate(baseUrl);
      await expect(replacement.waitForRequest({url: contains("/never")}, {timeoutMs: 100})).rejects.toMatchObject({code: "TARGET_NOT_FOUND"});
      expect(await otherTab.title()).toBe("");
    } finally {
      await isolatedBrowser?.close();
      await isolatedChrome.stop();
    }
  });

  it("captures raw screenshots and runs the visual baseline workflow through the real daemon", async () => {
    // Native controls can rasterize differently when Chrome moves a tab between foreground and
    // background. The full-page contract covers everything else; control captures are tested below.
    const nativeControlMasks = ["input", "select", "button"];
    await session.prepare();
    await session.navigate(baseUrl);
    const page = await session.captureScreenshot();
    expect([...page.subarray(0, 8)]).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
    const heading = await session.getByRole("heading", {name: "Biloba parity"}).captureScreenshot({mask: ["#count"]});
    expect([...heading.subarray(0, 8)]).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
    expect((await session.xpath(xpath("h1").withText("Biloba parity")).captureScreenshot()).byteLength).toBeGreaterThan(0);
    const path = await session.captureScreenshot({output: "path", name: "raw/page"});
    expect(path.startsWith(artifactDir)).toBe(true);
    expect((await readFile(path)).subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));

    // chromeConnection, not chromeWsUrl: a bare URL carries no launch metadata, so against full
    // headless Chrome the updater would skip the viewport emulation `session` gets and write
    // baselines at Chrome's un-emulated 1024x732 instead of 1024x768.
    const updater = await connect({daemonExecutable: daemonExecutable, chromeConnection: sharedBrowser.connection, artifactDir, screenshotBaselinesDir: baselineDir, updateScreenshots: true});
    const updateSession = await updater.openSession();
    await updateSession.navigate(baseUrl);
    const created = await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/heading");
    expect(created.schemes[0]?.status).toBe("created");
    expect((await updateSession.expectScreenshot("visual/page", {mask: nativeControlMasks})).schemes[0]?.status).toBe("created");
    const unchanged = await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/heading");
    expect(unchanged.schemes[0]?.status).toBe("unchanged");
    await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/changed");
    await updateSession.evaluate(`document.querySelector("h1").style.background = "rgb(0, 0, 255)"`);
    const updated = await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/changed");
    expect(updated.schemes[0]).toMatchObject({status: "updated", previousDiff: {match: false}});
    expect(updated.schemes[0]?.updateSummary).toContain("pixels changed");
    await updateSession.evaluate(`document.querySelector("h1").style.background = ""`);
    const schemes = await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/schemes", {colorSchemes: ["light", "dark"]});
    expect(schemes.schemes.map(({scheme, status}) => [scheme, status])).toEqual([["light", "created"], ["dark", "created"]]);
    await updateSession.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/masked", {mask: ["h1"]});
    await updateSession.evaluate(`document.body.insertAdjacentHTML("beforeend", '<div id="tolerance" style="width:20px;height:20px;background:rgb(0,0,0)"></div>')`);
    await updateSession.locator("#tolerance").expectScreenshot("visual/tolerance");
    await updater.close();

    const matched = await session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/heading");
    expect(matched.schemes[0]?.status).toBe("matched");
    // channelTolerance, not pixelTolerance: what varies between two captures of the same page is
    // rasterization noise - a small delta on many pixels. Chromium's inactive control outlines
    // vary by up to 16 channel levels even with their interiors masked. A pixel budget would
    // instead license a handful of pixels to change by any amount, which is exactly the small-glyph
    // or one-pixel-border regression worth catching.
    await expect(session.expectScreenshot("visual/page", {timeoutMs: 20_000, channelTolerance: 16, mask: nativeControlMasks})).resolves.toMatchObject({match: true});
    await expect(session.xpath(xpath("h1").withText("Biloba parity")).expectScreenshot("visual/heading")).resolves.toMatchObject({match: true});
    await expect(session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/missing", {timeoutMs: 500})).rejects.toMatchObject({
      code: "VISUAL_BASELINE",
      visual: {match: false, schemes: [{status: "missing"}]},
    });
    await session.evaluate(`document.querySelector("h1").style.background = "rgb(0, 255, 0)"`);
    await expect(session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/masked", {mask: ["h1"]})).resolves.toMatchObject({match: true});
    await session.evaluate(`document.querySelector("h1").style.background = ""`);
    await expect(session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/schemes", {colorSchemes: ["light", "dark"]})).resolves.toMatchObject({match: true});
    await session.evaluate(`document.body.insertAdjacentHTML("beforeend", '<div id="tolerance" style="position:relative;width:20px;height:20px;background:rgb(0,0,0)"><i style="position:absolute;width:1px;height:1px;background:white"></i></div>')`);
    await expect(session.locator("#tolerance").expectScreenshot("visual/tolerance", {timeoutMs: 50, intervalMs: 10})).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#tolerance").expectScreenshot("visual/tolerance", {pixelTolerance: 0.01})).resolves.toMatchObject({match: true});
    await session.evaluate(`document.querySelector("#tolerance").innerHTML = ""; document.querySelector("#tolerance").style.background = "rgb(1,1,1)"`);
    await expect(session.locator("#tolerance").expectScreenshot("visual/tolerance", {timeoutMs: 50, intervalMs: 10})).rejects.toMatchObject({code: "TIMEOUT"});
    await expect(session.locator("#tolerance").expectScreenshot("visual/tolerance", {channelTolerance: 1})).resolves.toMatchObject({match: true});
    await session.evaluate(`document.querySelector("h1").style.background = "rgb(255, 0, 0)"`);
    let mismatch: unknown;
    try { await session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/heading", {timeoutMs: 100, intervalMs: 10}); } catch (error) { mismatch = error; }
    expect(mismatch).toMatchObject({
      code: "TIMEOUT",
      visual: {match: false, attemptCount: expect.any(Number), schemes: [{status: "mismatched", diagnosis: expect.stringContaining("differs from baseline")}]},
    });
    expect((mismatch as BilobaError).artifactPaths.some((entry) => entry.endsWith(".actual.png"))).toBe(true);
    expect((mismatch as BilobaError).artifactPaths.some((entry) => entry.endsWith(".diff.png"))).toBe(true);
    // The diagnosis and the artifact paths have to be in the message.  Carried only as error
    // properties they are invisible: Vitest renders message and stack, so a mismatch read as
    // "did not match" while the diff PNG sat on disk unmentioned.
    const rendered = (mismatch as BilobaError).message;
    expect(rendered).toContain("differs from baseline");
    expect(rendered).toMatch(/^diff: .*\.diff\.png$/m);
    expect(rendered).toMatch(/^actual: .*\.actual\.png$/m);
    expect(rendered).toMatch(/^baseline: .*\.png$/m);
  });

  it("records page errors and Chrome's own console messages", async () => {
    await session.prepare();
    await session.navigate(baseUrl);
    const live: string[] = [];
    const stop = session.onPageError((pageError) => live.push(pageError.message));
    await session.evaluate(`setTimeout(function explode() { throw new TypeError("parity boom") })`);
    const pageError = await session.expectPageError(contains("parity boom"), {timeoutMs: 2_000});
    expect(pageError.message).toBe("Uncaught TypeError: parity boom");
    expect(pageError.stack[0]).toMatchObject({functionName: "explode"});
    expect(pageError.line).toBeGreaterThanOrEqual(0);
    expect((await session.pageErrors()).map(({message}) => message)).toEqual(["Uncaught TypeError: parity boom"]);
    await expect.poll(() => live, {timeout: 2_000}).toEqual(["Uncaught TypeError: parity boom"]);
    stop();

    await session.evaluateAsync(`fetch("/not-found").then(response => response.text())`);
    await session.evaluate(`console.error("page's own error")`);
    await expect.poll(async () => (await session.consoleMessages()).map(({source, type}) => `${source}:${type}`), {timeout: 2_000}).toEqual(expect.arrayContaining(["network:error", ":error"]));

    await session.prepare();
    expect(await session.pageErrors()).toEqual([]);
  });

  it("preserves capture warnings and visual lifecycle state across cancellation", async () => {
    screenshotWarnings.length = 0;
    await session.prepare();
    await session.navigate(baseUrl);
    await session.evaluate(`document.body.insertAdjacentHTML("beforeend", '<div id="clip" style="width:20px;height:20px;overflow:hidden"><div id="clipped" style="margin-top:40px;width:10px;height:10px;background:red"></div></div>')`);
    const clipped = await session.locator("#clipped").captureScreenshot();
    expect(clipped.byteLength).toBeGreaterThan(0);
    expect(screenshotWarnings.some((warning) => warning.includes("clipped"))).toBe(true);

    await session.evaluate(`document.body.insertAdjacentHTML("beforeend", '<style>@keyframes visual-red {from{background:#a00}to{background:#a00}} #animated-visual{width:40px;height:30px;background:#0a0;animation:visual-red 100000s linear forwards}</style><div id="animated-visual"></div>')`);
    // A plain capture leaves the page running, as Go's does; frozen is expectScreenshot's freeze.
    const plain = await session.locator("#animated-visual").captureScreenshot();
    const frozen = await session.locator("#animated-visual").captureScreenshot({frozen: true});
    expect(plain).not.toEqual(frozen);
    expect(await session.evaluate(`document.querySelectorAll("#_biloba-freeze").length`)).toBe(0);
    expect(await session.locator("#animated-visual").captureScreenshot({hideCaret: true})).toEqual(plain);
    expect(await session.evaluate(`document.querySelectorAll("#_biloba-caret").length`)).toBe(0);

    const {width, height} = await session.evaluate<{width: number; height: number}>(`({width: Math.round(visualViewport.width), height: Math.round(visualViewport.height)})`);
    expect(pngSize(await session.captureScreenshot({viewport: true}))).toEqual({width, height});
    expect(pngSize(await session.captureScreenshot({clip: {x: 2, y: 3, width: 30, height: 20}}))).toEqual({width: 30, height: 20});
    await expect(session.captureScreenshot({clip: {x: 0, y: height - 5, width: 10, height: 10}})).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("reaches outside")});
    await session.evaluate(`document.body.insertAdjacentHTML("beforeend", '<div id="below-fold" style="margin-top:3000px;width:10px;height:10px"></div>')`);
    await expect(session.locator("#below-fold").captureScreenshot({viewport: true})).rejects.toMatchObject({message: expect.stringContaining("not entirely in the viewport")});
    await session.evaluate(`document.querySelector("#below-fold").remove()`);

    await session.setMedia({colorScheme: "dark", reducedMotion: "reduce"});
    const before = await session.evaluate(`[matchMedia("(prefers-color-scheme: dark)").matches, matchMedia("(prefers-reduced-motion: reduce)").matches]`);
    expect(before).toEqual([true, true]);
    await expect(session.getByRole("heading", {name: "Biloba parity"}).expectScreenshot("visual/schemes", {colorSchemes: ["light", "dark"]})).resolves.toMatchObject({match: true});
    expect(await session.evaluate(`[matchMedia("(prefers-color-scheme: dark)").matches, matchMedia("(prefers-reduced-motion: reduce)").matches]`)).toEqual([true, true]);

    await session.evaluate(`_biloba.fontsReady = () => new Promise(() => {})`);
    const controller = new AbortController();
    setTimeout(() => controller.abort("cancel visual capture"), 30);
    await expect(session.captureScreenshot({signal: controller.signal, timeoutMs: 2_000})).rejects.toMatchObject({code: "CANCELLED"});
    await session.navigate(baseUrl);
    expect(await session.evaluate("1 + 1")).toBe(2);
  });

  describe("the pointer", () => {
    type PointerEvent = {type: string; pointerType: string; buttons: number; x: number; y: number; pressure: number};
    const pointerLog = async (on: Session) => await on.evaluate<PointerEvent[]>("window.pointerLog");
    // On Linux, chrome-headless-shell moves the mouse to (0, 0) when a page from a new site renders its
    // first frame, so the log starts once the page has rendered two (see settlePointerLog in pointer_test.go).
    const settle = async (on: Session) => { await on.expectEvaluation("window.framesRendered", true); await on.evaluate("window.pointerLog = []"); };
    const event = (type: string, pointerType: string, buttons: number, x: number, y: number) => expect.objectContaining({type, pointerType, buttons, x: expect.closeTo(x, 0), y: expect.closeTo(y, 0)});
    let tab: Session;

    beforeAll(async () => { tab = await browser.openSession(); });
    afterAll(async () => { await tab.close(); });

    it("dispatches real moves, presses, and releases, with buttons=1 while pressed", async () => {
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      const p = tab.pointer();
      await p.moveTo(30, 30);
      await p.down();
      await p.moveTo(60, 40);
      await p.moveTo(90, 50);
      await p.up();
      await p.moveTo(100, 60);
      expect(await pointerLog(tab)).toEqual([
        event("pointermove", "mouse", 0, 30, 30), event("pointerdown", "mouse", 1, 30, 30), event("pointermove", "mouse", 1, 60, 40),
        event("pointermove", "mouse", 1, 90, 50), event("pointerup", "mouse", 0, 90, 50), event("pointermove", "mouse", 0, 100, 60),
      ]);
    });

    it("dispatches pen events with pressure while the pen touches", async () => {
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      const pen = tab.pointer({type: "pen"});
      await pen.moveTo(30, 30);
      await pen.down();
      await pen.moveTo(60, 40);
      await pen.up();
      const log = await pointerLog(tab);
      expect(log).toEqual([event("pointermove", "pen", 0, 30, 30), event("pointerdown", "pen", 1, 30, 30), event("pointermove", "pen", 1, 60, 40), event("pointerup", "pen", 0, 60, 40)]);
      expect(log.map(({pressure}) => pressure)).toEqual([0, 0.5, 0.5, 0]);
    });

    it("moves to elements, polling for one that shows up late, and drags a slider", async () => {
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      const p = tab.pointer();
      await p.moveToElement("#pad");
      await p.moveToElement(tab.locator("#pad"), {position: {x: 5, y: 10}});
      await tab.evaluate(`setTimeout(() => { let d = document.createElement("div"); d.id = "late"; d.style.cssText = "position:absolute;left:300px;top:300px;width:40px;height:40px"; document.body.appendChild(d) }, 200)`);
      await p.moveToElement("#late");
      expect(await pointerLog(tab)).toEqual([event("pointermove", "mouse", 0, 120, 60), event("pointermove", "mouse", 0, 25, 30), event("pointermove", "mouse", 0, 320, 320)]);

      await p.moveToElement("#track", {position: {x: 0, y: 10}});
      await p.down();
      await p.moveTo(70, 170);
      expect(await tab.locator("#track").getAttribute("data-value")).toBe("25");
      await p.moveTo(170, 170);
      await p.up();
      expect(await tab.locator("#track").getAttribute("data-value")).toBe("75");
    });

    it("does not scroll, fails on a missing element at the deadline, and refuses a second press", async () => {
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      const p = tab.pointer();
      await expect(p.moveToElement("#far", {immediate: true})).rejects.toMatchObject({code: "TIMEOUT", daemonDetail: expect.stringContaining("the pointer does not scroll")});
      expect(await tab.evaluate("window.scrollY")).toBe(0);
      await expect(p.moveToElement("#missing", {timeoutMs: 150})).rejects.toMatchObject({code: "TIMEOUT"});
      await expect(p.up()).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("the pointer is not pressed")});
      await p.down();
      await expect(p.down()).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("the pointer is already pressed")});
      await p.up();
      expect(await pointerLog(tab)).toEqual([event("pointerdown", "mouse", 1, 0, 0), event("pointerup", "mouse", 0, 0, 0)]);
    });

    it("works in a cross-origin frame's viewport", async () => {
      await tab.navigate(baseUrl);
      await tab.evaluate(`url => { document.body.innerHTML = '<iframe style="position:absolute;left:100px;top:120px;width:360px;height:260px;border:6px solid black"></iframe>'; document.querySelector("iframe").src = url }`, [`${childBaseUrl}/pointer.html`]);
      const frame = await tab.waitForFrame({has: "#pad"}, {timeoutMs: 5_000});
      await settle(frame);
      const p = frame.pointer();
      await p.moveTo(30, 30);
      await p.down();
      await p.moveToElement("#pad", {position: {x: 100, y: 40}});
      await p.up();
      expect(await pointerLog(frame)).toEqual([event("pointermove", "mouse", 0, 30, 30), event("pointerdown", "mouse", 1, 30, 30), event("pointermove", "mouse", 1, 120, 60), event("pointerup", "mouse", 0, 120, 60)]);
      await expect(p.moveToElement(tab.locator("#pad"))).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    });

    it("is released by prepare() when a test leaves it pressed, and starts again at (0, 0)", async () => {
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      await tab.evaluate(`document.addEventListener("pointerup", () => navigator.sendBeacon("/released"))`);
      pointerReleases = 0;
      const p = tab.pointer();
      await p.moveTo(40, 40);
      await p.down();
      await tab.prepare();
      await expect.poll(() => pointerReleases).toBe(1);
      await tab.navigate(`${baseUrl}/pointer.html`);
      await settle(tab);
      await p.down();
      await p.up();
      expect(await pointerLog(tab)).toEqual([event("pointerdown", "mouse", 1, 0, 0), event("pointerup", "mouse", 0, 0, 0)]);
    });
  });

  describe("screencasts", () => {
    let tab: Session;
    beforeAll(async () => { tab = await browser.openSession(); });
    afterAll(async () => { await tab.close(); });

    it("records an animating page's frames, timestamped by Chrome, in order, and stops once", async () => {
      await tab.navigate(`${baseUrl}/screencast.html`);
      const before = Date.now();
      const recording = await tab.startScreencast({format: "jpeg", quality: 92});
      await new Promise((resolve) => setTimeout(resolve, 500));
      const frames = await recording.stop();
      const after = Date.now();
      expect(frames.length).toBeGreaterThanOrEqual(2);
      const size = await tab.windowSize();
      frames.forEach((frame, index) => {
        expect([...frame.data.subarray(0, 2)]).toEqual([0xff, 0xd8]);
        expect([frame.deviceWidth, frame.deviceHeight, frame.pageScaleFactor, frame.scrollOffsetY]).toEqual([size.width, size.height, 1, 0]);
        // From Chrome 156 a frame is stamped with when Chrome expects to display it, about one frame
        // after it was drawn, so the last frame's timestamp can be later than stop() resolving.
        expect(frame.timestamp).toBeLessThanOrEqual(after + 50);
        // The first frame is the one already on screen, so it can predate startScreencast.
        if (index > 0) {
          expect(frame.timestamp).toBeGreaterThanOrEqual(before - 1);
          expect(frame.timestamp).toBeGreaterThanOrEqual(frames[index - 1]!.timestamp);
        }
      });
      expect(await recording.stop()).toBe(frames);
    });

    it("shrinks PNG frames to fit maxWidth and maxHeight", async () => {
      await tab.navigate(`${baseUrl}/screencast.html`);
      const recording = await tab.startScreencast({format: "png", maxWidth: 300, maxHeight: 150});
      await new Promise((resolve) => setTimeout(resolve, 300));
      const frames = await recording.stop();
      const last = frames[frames.length - 1]!;
      expect([...last.data.subarray(1, 4)]).toEqual([0x50, 0x4e, 0x47]);
      const size = pngSize(last.data);
      expect(size.width <= 300 && size.height <= 150 && (size.width === 300 || size.height === 150)).toBe(true);
    });

    it("records one screencast per tab at a time, refuses frame handles, and is discarded by prepare()", async () => {
      await tab.navigate(`${baseUrl}/screencast.html`);
      const running = await tab.startScreencast();
      await expect(tab.startScreencast()).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("already recording a screencast")});
      const other = await tab.newTab();
      await other.navigate(`${baseUrl}/screencast.html`);
      const otherRecording = await other.startScreencast();
      await new Promise((resolve) => setTimeout(resolve, 300));
      expect((await otherRecording.stop()).length).toBeGreaterThan(0);
      expect((await running.stop()).length).toBeGreaterThan(0);

      await tab.evaluate(`url => { const frame = document.createElement("iframe"); frame.src = url; document.body.append(frame) }`, [`${childBaseUrl}/pointer.html`]);
      const frame = await tab.waitForFrame({has: "#pad"}, {timeoutMs: 5_000});
      await expect(frame.startScreencast()).rejects.toMatchObject({code: "INVALID_ARGUMENT"});

      const forgotten = await tab.startScreencast();
      await tab.prepare();
      await expect(forgotten.stop()).rejects.toMatchObject({code: "TARGET_NOT_FOUND", message: expect.stringContaining("discards its recordings when it is prepared or closed")});
      await (await tab.startScreencast()).stop();
    });
  });

  describe("the page clock", () => {
    const t0 = new Date(Date.UTC(2024, 1, 2, 8));
    const at = (ms: number) => t0.getTime() + ms;
    const pausedNote = "This tab's page clock is paused at 2024-02-02T08:00:00.000Z; timers and animation frames won't run until you call runFor, fastForward, or resume.";
    const text = async (on: Session, selector: string) => await on.locator(selector).innerText();
    const onRealTime = async (on: Session) => {
      expect(await on.evaluate("typeof window._bilobaClock")).toBe("undefined");
      expect(Math.abs(await on.evaluate<number>("Date.now()") - Date.now())).toBeLessThan(5_000);
    };
    let tab: Session;

    beforeAll(async () => { tab = await browser.openSession(); });
    beforeEach(async () => { await tab.prepare(); });
    afterAll(async () => { await tab.close(); });

    const pausedPage = async () => {
      const clock = await tab.installClock(t0);
      await clock.pauseAt(t0);
      await tab.navigate(`${baseUrl}/clock.html`);
      return clock;
    };

    it("starts the page where it was paused and runs its timers and animation frames only when told to", async () => {
      const clock = await pausedPage();
      expect(await text(tab, "#now")).toBe("2024-02-02T08:00:00.000Z");
      await new Promise((resolve) => setTimeout(resolve, 200));
      expect([await text(tab, "#countdown"), await text(tab, "#frames")]).toEqual(["10", "0"]);

      await clock.runFor(300);
      expect([await text(tab, "#chain"), await text(tab, "#frames"), await text(tab, "#later")]).toEqual(["one two three", "18", ""]);
      await clock.runFor(4_700);
      expect([await text(tab, "#countdown"), await text(tab, "#later")]).toEqual(["5", "later"]);
      expect(await tab.evaluate("Date.now()")).toBe(at(5_000));
    });

    it("fires each due timer once with fastForward, pins Date with setFixedTime, and moves it with setSystemTime", async () => {
      const clock = await pausedPage();
      await clock.fastForward(3_600_000);
      expect([await text(tab, "#countdown"), await text(tab, "#ago")]).toEqual(["9", "60 minutes ago"]);
      expect(await tab.evaluate("Date.now()")).toBe(at(3_600_000));

      const fixed = Date.UTC(2030, 0, 1);
      await clock.setFixedTime(new Date(fixed));
      await clock.runFor(1_000);
      expect(await text(tab, "#countdown")).toBe("8");
      expect(await tab.evaluate("[Date.now(), new Date().getTime()]")).toEqual([fixed, fixed]);

      await clock.setSystemTime(at(-3_600_000));
      expect(await tab.evaluate("Date.now()")).toBe(at(-3_600_000));
      await clock.runFor(999);
      expect(await text(tab, "#countdown")).toBe("8");
      await clock.runFor(1);
      expect(await text(tab, "#countdown")).toBe("7");
    });

    it("hands back the installed clock from session.clock(), and lets time flow again with resume", async () => {
      await pausedPage();
      await tab.clock().runFor(1_000);
      expect(await text(tab, "#countdown")).toBe("9");
      await tab.clock().resume();
      await tab.locator("#countdown").expectText("8", {timeoutMs: 3_000});
      await expect.poll(async () => Number(await text(tab, "#frames"))).toBeGreaterThan(5);
    });

    it("bounds runFor by real time and says how far it got", async () => {
      const clock = await pausedPage();
      await tab.evaluate("setInterval(() => {}, 1)");
      await expect(clock.runFor(3_600_000, {timeoutMs: 50})).rejects.toMatchObject({code: "TIMEOUT", message: expect.stringMatching(/runFor\(3600000\) did not finish within 50ms[\s\S]*fastForward skips ahead/)});
    });

    it("says the clock is paused when a poll times out, and not for an immediate read or once time flows", async () => {
      const clock = await pausedPage();
      const assertion = await tab.locator("#countdown").expectText("9", {timeoutMs: 100}).catch((reason: unknown) => reason);
      expectTimedOutAssertion(assertion, {locator: `locator("#countdown")`, expected: "9"});
      expect(assertion.message).toContain(pausedNote);
      const action = await tab.locator("#not-there").click({timeoutMs: 100}).catch((reason: unknown) => reason);
      expectTimedOutAction(action, {locator: `locator("#not-there")`, operation: "click"});
      expect(action.message).toContain(pausedNote);

      await expect(tab.locator("#countdown").expectText("9", {immediate: true})).rejects.toSatisfy((error: Error) => !error.message.includes("page clock"));
      await clock.resume();
      await expect(tab.locator("#not-there").click({timeoutMs: 100})).rejects.toSatisfy((error: Error) => error.message.includes("timed out") && !error.message.includes("page clock"));
    });

    it("covers the tab's same-process frames, which refuse to control it", async () => {
      const clock = await tab.installClock(t0);
      await clock.pauseAt(t0);
      await tab.navigate(baseUrl);
      await tab.evaluate(`url => { const frame = document.createElement("iframe"); frame.src = url; document.body.append(frame) }`, [`${childBaseUrl}/clock.html`]);
      const frame = await tab.waitForFrame({has: "#countdown"}, {timeoutMs: 5_000});
      expect(await text(frame, "#now")).toBe("2024-02-02T08:00:00.000Z");
      await clock.runFor(5_000);
      expect([await text(frame, "#countdown"), await text(frame, "#later")]).toEqual(["5", "later"]);

      const failure = await frame.locator("#countdown").expectText("4", {timeoutMs: 100}).catch((reason: unknown) => reason);
      expect((failure as Error).message).toContain("This tab's page clock is paused at 2024-02-02T08:00:05.000Z");
      await expect(frame.installClock(t0)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await expect(frame.clock().runFor(1_000)).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
    });

    it("is per tab", async () => {
      const other = await tab.newTab();
      try {
        await (await other.installClock(t0)).pauseAt(t0);
        await other.navigate(`${baseUrl}/clock.html`);
        expect(await text(other, "#now")).toBe("2024-02-02T08:00:00.000Z");
        await tab.navigate(`${baseUrl}/clock.html`);
        await onRealTime(tab);
      } finally {
        await other.close();
      }
    });

    it("refuses a second install and calls without one, and prepare() removes it", async () => {
      await expect(tab.clock().runFor(1)).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("this tab has no page clock")});
      await pausedPage();
      await expect(tab.installClock(t0)).rejects.toMatchObject({code: "INVALID_ARGUMENT", message: expect.stringContaining("already installed")});
      expect(await tab.evaluate("Date.now()")).toBe(t0.getTime());

      await tab.prepare();
      await tab.navigate(`${baseUrl}/clock.html`);
      await onRealTime(tab);
      await expect(tab.clock().resume()).rejects.toMatchObject({code: "INVALID_ARGUMENT"});
      await tab.installClock(t0);
    });
  });

  afterAll(async () => {
    await browser?.close();
    await sharedBrowser?.stop();
    if (server) {
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
    if (childServer) {
      await new Promise<void>((resolve, reject) => childServer.close((error) => error ? reject(error) : resolve()));
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

  // Chrome answers Fetch.enable before the renderer has the intercepting loader factory, and a busy
  // page runs a keydown ahead of that update - so an input-started fetch right after the session's
  // first registration used to escape.  prepare() turns interception off, so every round registers
  // the first handler again.
  it("intercepts a request that keyboard input starts straight after the first registration", async () => {
    for (let round = 0; round < 25; round++) {
      await session.prepare();
      await session.navigate(baseUrl);
      await session.evaluate(`(() => {
        const until = performance.now() + 2000
        let pressed = false
        window.addEventListener("keydown", () => { pressed = true; fetch("/network-json?pressed", {method: "POST"}) })
        const busy = () => {
          const t = performance.now(); while (performance.now() - t < 14) {}
          if (!pressed && performance.now() < until) requestAnimationFrame(busy)
        }
        busy()
      })()`);
      const hold = await session.holdResponse(endsWith("/network-json?pressed"));
      await session.sendKeys("a");
      await hold.await({timeoutMs: 1_000});
      await hold.release();
    }
  });

  it("reads the response body before starting the callback timeout", async () => {
    await session.prepare();
    await session.navigate(baseUrl);
    const route = await session.routeResponse(endsWith("/slow-callback"), (response) => ({
      status: 202,
      body: new TextEncoder().encode(new TextDecoder().decode(response.body).toUpperCase()),
    }), {timeoutMs: 200});
    try {
      expect(await session.evaluateAsync(`fetch("/slow-callback").then(async response => [response.status, await response.text()])`)).toEqual([202, "SLOW BODY"]);
      expect((await route.stats()).lastError).toBeFalsy();
    } finally {
      await route.remove();
    }
  });

  it("continues redirects whose response body Chrome refuses to transfer", async () => {
    await session.prepare();
    await session.navigate(baseUrl);
    let invoked = 0;
    const route = await session.routeResponse(endsWith("/redirect-response"), () => {
      invoked++;
      return {status: 204};
    });
    try {
      expect(await session.evaluateAsync(`fetch("/redirect-response").then(response => response.text())`)).toBe("redirect final");
      expect(invoked).toBe(0);
      expect((await route.stats()).lastError).toContain("response body");
    } finally {
      await route.remove();
    }
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

    // A callback replaces the response, as ResponseModification.Using does: an unset status means
    // 200, and an unset body means none.  Patching instead handed the page a 204 that still carried
    // the original body - something a response is never allowed to be.
    const statusOnly = await session.routeResponse(endsWith("/callback"), () => ({status: 204}));
    expect(await session.evaluateAsync(`fetch("/callback").then(async response => [response.status, await response.text()])`)).toEqual([204, ""]);
    await statusOnly.remove();
    const bodyOnly = await session.routeResponse(endsWith("/callback"), () => ({body: new TextEncoder().encode("replaced")}));
    expect(await session.evaluateAsync(`fetch("/callback").then(async response => [response.status, await response.text()])`)).toEqual([200, "replaced"]);
    await bodyOnly.remove();
    // An explicitly empty replacement body has to reach Chrome as an empty body.  Fetch.fulfillRequest
    // reads an absent body as "reuse the original response body", and at the response stage that body
    // has already been taken as a stream - so the request was never resolved and the page hung.
    const emptyBody = await session.routeResponse(endsWith("/callback"), () => ({status: 204, body: new Uint8Array()}));
    expect(await session.evaluateAsync(`fetch("/callback").then(async response => [response.status, await response.text()])`)).toEqual([204, ""]);
    await emptyBody.remove();
    // The same holds when the real response is the empty one: a transform that keeps its body, or a
    // hold that releases it, still has to send that empty body explicitly (onsi/biloba#45).
    const emptyOriginal = await session.routeResponse(endsWith("/empty"), (response) => ({status: response.status, headers: [{name: "x-touched", value: "1"}], body: response.body}));
    expect(await session.evaluateAsync(`fetch("/empty", {method: "POST"}).then(async response => [response.status, response.headers.get("x-touched"), await response.text()])`)).toEqual([202, "1", ""]);
    await emptyOriginal.remove();
    const emptyHold = await session.holdResponse(endsWith("/empty"));
    await session.evaluate(`{ window.__emptyHeld = fetch("/empty", {method: "POST"}).then(async response => [response.status, await response.text()]); }`);
    expect(new TextDecoder().decode((await emptyHold.await({timeoutMs: 2_000})).body)).toBe("");
    await emptyHold.release();
    expect(await session.evaluateAsync(`window.__emptyHeld`)).toEqual([202, ""]);

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
    const observed = await session.requests({url: endsWith("/observed")});
    expect(observed[0]).toMatchObject({status: 200, failure: ""});
    await session.evaluateAsync(`Promise.all([fetch("/not-found").then(response => response.text()), fetch("http://127.0.0.1:1/unreachable").catch(() => null)])`);
    const failed = await session.waitForRequest({url: endsWith("/unreachable"), failed: true}, {timeoutMs: 1_000});
    expect(failed.url).toContain("/unreachable");
    expect(failed.failure).toMatch(/^net::ERR_/);
    expect(failed.status).toBe(0);
    expect((await session.requests({status: 404})).map(({url}) => new URL(url).pathname)).toContain("/not-found");
    expect((await session.responses({status: 404})).map(({url}) => new URL(url).pathname)).toContain("/not-found");
    expect((await session.responses({status: 404})).every(({status}) => status === 404)).toBe(true);
    expect(await session.requests({url: endsWith("/not-found"), failed: true})).toEqual([]);

    const idleHold = await session.holdResponse(contains("/network-json?idle"));
    await session.evaluate(`void fetch("/network-json?idle")`);
    await idleHold.await({timeoutMs: 2_000});
    const busy = await session.expectNetworkIdle({timeoutMs: 150}).catch((error: unknown) => error);
    expect(busy).toBeInstanceOf(BilobaError);
    expect((busy as BilobaError).code).toBe("TIMEOUT");
    expect((busy as BilobaError).message).toMatch(/1 in-flight request\(s\)\n {2}GET http:\/\/\S+\/network-json\?idle \(Fetch, in flight for \d+ms\)/);
    await idleHold.releaseAll();

    const idle = await session.expectNetworkIdle({timeoutMs: 1_000});
    expect(idle.attemptCount).toBeGreaterThan(0);
    // The trajectory is the poll's own attempts. Reporting it empty made this a typed result whose
    // diagnostic half was a fiction, so "the network never went idle" said nothing about what the
    // in-flight count had been doing.
    expect(idle.trajectory).toHaveLength(idle.attemptCount);
    expect(idle.trajectory[0]).toMatchObject({attempt: 1, elapsedMs: expect.any(Number)});

    await session.setOffline(true);
    expect(await session.networkState()).toMatchObject({offline: true});
    expect(await session.evaluate("navigator.onLine")).toBe(false);
    await session.resetNetworkState();
    expect(await session.evaluateAsync(`fetch("/network-json").then(response => response.text())`)).toBe("network");
    await session.setNetworkState({latencyMs: 20, downloadThroughput: 64_000, uploadThroughput: 32_000, connectionType: "wifi"});
    expect(await session.networkState()).toMatchObject({latencyMs: 20, downloadThroughput: 64_000, uploadThroughput: 32_000, connectionType: "wifi"});
    await session.resetNetworkState();
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
      await warningSession.getByRole("button", {name: "Increment"}).click();
      await warningSession.evaluate(`window.addEventListener("beforeunload", event => { event.preventDefault(); event.returnValue = "leave"; })`);
      await warningSession.navigate(`${baseUrl}/?after-beforeunload=1`);
      expect(warnings.map(({message}) => message)).toContainEqual(expect.stringContaining("beforeunload"));
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
    const startingEmulation = await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches, innerWidth, innerHeight, devicePixelRatio, navigator.userAgent, navigator.maxTouchPoints, screen.orientation.angle]`);
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
    // Permission names are navigator.permissions.query's: Chrome rejects CDP's internal ones.
    await session.setPermissions(baseUrl, {camera: "granted", microphone: "denied", "clipboard-read": "granted"});
    expect(await session.evaluateAsync(`Promise.all(["camera", "microphone", "clipboard-read"].map(name => navigator.permissions.query({name}).then(result => result.state)))`)).toEqual(["granted", "denied", "granted"]);
    await session.setLocale("fr-FR");
    await session.setTimezone("Europe/Paris");
    await session.setMedia({colorScheme: "dark", reducedMotion: "reduce"});
    await session.setDeviceMetrics({width: 320, height: 640, deviceScaleFactor: 2, mobile: true, touch: true, landscape: true});
    await session.setUserAgent("BilobaPhone/1.0");
    expect(await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches]`)).toEqual(["fr-FR", "Europe/Paris", true]);
    expect(await session.evaluate(`[screen.width, screen.height, devicePixelRatio, innerWidth]`)).toEqual([320, 640, 2, 980]);
    expect(await session.evaluate(`[navigator.maxTouchPoints > 0, screen.orientation.type, screen.orientation.angle, navigator.userAgent]`)).toEqual([true, "landscape-primary", 90, "BilobaPhone/1.0"]);
    expect(await session.evaluateAsync<string>(`fetch("/user-agent").then(response => response.text())`)).toBe("BilobaPhone/1.0");
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
    expect(await session.evaluate(`[Intl.NumberFormat().resolvedOptions().locale, Intl.DateTimeFormat().resolvedOptions().timeZone, matchMedia('(prefers-color-scheme: dark)').matches, innerWidth, innerHeight, devicePixelRatio, navigator.userAgent, navigator.maxTouchPoints, screen.orientation.angle]`)).toEqual(startingEmulation);
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

  it("can try an action exactly once through the immediate alias", async () => {
    await session.prepare();
    await session.navigate(baseUrl);

    const failure = await session.locator("#never").click({immediate: true})
      .catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(BilobaError);
    const error = failure as BilobaError;
    expect(error.code).toBe("TIMEOUT");
    expect(error.trajectory).toHaveLength(1);
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

function pngSize(png: Uint8Array): {width: number; height: number} {
  const view = new DataView(png.buffer, png.byteOffset, png.byteLength);
  return {width: view.getUint32(16), height: view.getUint32(20)};
}
