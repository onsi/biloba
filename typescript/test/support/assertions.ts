import {expect} from "vitest";

import {BilobaError} from "../../src/index.js";

/**
 * The contract a timed-out Biloba operation must satisfy, asserted identically against the stub
 * daemon in client.test.ts and against a real bilobad in the parity suite.
 *
 * This is the answer to "what stops the stub from lying?".  Generated types constrain the *shape*
 * of what the stub sends; nothing constrains its *behaviour* - a stub is free to claim a failed
 * click succeeded, and no type will notice.  Sharing the assertion means the fast stub tests and
 * the slow real-daemon tests are held to one definition: if the stub drifts from the daemon on
 * anything asserted here, the parity run fails on the same line.
 *
 * Keep this about the invariants both can honour.  Anything that depends on a specific payload
 * (an exact observed value, a screenshot path) belongs at the call site.
 */
export function expectTimedOutOperation(
  error: unknown,
  expectations: {locator: string; expected?: string; summary: string},
): asserts error is BilobaError {
  expect(error, "a Biloba operation that exhausts its budget must reject with a BilobaError").toBeInstanceOf(BilobaError);
  const failure = error as BilobaError;

  expect(failure.code).toBe("TIMEOUT");
  expect(failure.locator).toBe(expectations.locator);
  if (expectations.expected !== undefined) expect(failure.expected).toBe(expectations.expected);

  // One protocol request, however many times the daemon polled inside it.  This is the whole
  // premise of server-side polling; a client-side retry loop would show up here.
  expect(failure.rpcRequestCount).toBe(1);
  expect(failure.rpcResponseCount).toBe(1);

  // The message has to name what was being looked for and how hard it was tried - that is what
  // makes a timeout actionable rather than just late.
  expect(failure.message).toContain(expectations.summary);
  expect(failure.message).toContain(`locator: ${expectations.locator}`);
  expect(failure.message).toMatch(/attempts: \d+/);

  expect(failure.trajectory.length, "a timeout must carry the attempts it made").toBeGreaterThan(0);
  for (const observation of failure.trajectory) {
    expect(observation.attempt).toBeGreaterThan(0);
    expect(typeof observation.elapsedMs).toBe("number");
  }
}

/** The assertion form: `expectText`, `expectVisible`, and friends. */
export function expectTimedOutAssertion(error: unknown, expectations: {locator: string; expected?: string}): asserts error is BilobaError {
  expectTimedOutOperation(error, {...expectations, summary: "Biloba assertion timed out"});
}

/** The action form: `click`, `setValue`. */
export function expectTimedOutAction(
  error: unknown,
  expectations: {locator: string; operation: "click" | "setValue"},
): asserts error is BilobaError {
  expectTimedOutOperation(error, {
    locator: expectations.locator,
    expected: "operation to succeed",
    summary: `Biloba ${expectations.operation} operation timed out`,
  });
}
