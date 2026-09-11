import {afterAll, beforeAll, beforeEach, inject, it} from "vitest";
import {connect, type Browser, type Session} from "biloba";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeConnection: inject("chromeConnection")});
  session = await browser.openSession();
});
afterAll(async () => { await browser.close(); });
beforeEach(async () => { await session.prepare(); });

it("adds a todo", async () => {
  await session.navigate(inject("baseUrl"));
  await session.getByPlaceholder("What needs doing?").setValue("Buy milk");
  await session.getByRole("button", {name: "Add"}).click();
  await session.getByText("Buy milk", {exact: true}).expectVisible();
});
