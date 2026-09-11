import {readFile} from "node:fs/promises";
import {createServer, type Server} from "node:http";
import type {AddressInfo} from "node:net";
import {startSharedBrowser, type SharedBrowserConnection, type SharedBrowserProcess} from "biloba";
import type {TestProject} from "vitest/node";

declare module "vitest" {
  export interface ProvidedContext {
    chromeConnection: SharedBrowserConnection;
    baseUrl: string;
  }
}

let browser: SharedBrowserProcess | undefined;
let server: Server | undefined;

export async function setup(project: TestProject) {
  // The app under test: one static page.
  const page = await readFile(new URL("./todos.html", import.meta.url));
  server = createServer((_request, response) => {
    response.setHeader("content-type", "text/html");
    response.end(page);
  });
  await new Promise<void>((resolve) => server!.listen(0, "127.0.0.1", resolve));
  project.provide("baseUrl", `http://127.0.0.1:${(server.address() as AddressInfo).port}`);

  browser = await startSharedBrowser({mode: "headless-shell"}); // no executable: resolved from the platform package
  project.provide("chromeConnection", browser.connection);
}

export async function teardown() {
  await browser?.stop();
  server?.close();
}
