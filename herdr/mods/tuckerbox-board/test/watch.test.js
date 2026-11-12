// Watch end to end inside the mod (agent-terminals: Watch): the card's
// Watch button goes through invocation and perform, which call Herdr's
// agent.focus with the agent's terminal. Herdr's "herdr" module is stubbed
// so perform runs under node.   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { register } from "node:module";
import { readFileSync } from "node:fs";

const calls = (globalThis.__herdrCalls = []);
const stub = `
export function run(command, args) { globalThis.__herdrCalls.push({ command, args }); }
export function spaces() { return [{ id: "s1", machine: null, active: true }]; }
export function panes() { return []; }
export function focused() { return { space: "s1" }; }
export async function exec() { throw new Error("Watch runs no command"); }
export async function hc() { throw new Error("Watch runs no command"); }
export function readTextFile() { return ""; }
export function appHome() { return "/home"; }
export function props() { return {}; }
export function machines() { return []; }
`;
// A resolve hook (module.register, node 20.6 and later) maps "herdr" to the
// stub; the stub itself runs in this thread, so its calls land here.
const loader = `
export async function resolve(specifier, context, next) {
  if (specifier === "herdr") return { url: ${JSON.stringify("data:text/javascript," + encodeURIComponent(stub))}, shortCircuit: true };
  return next(specifier, context);
}
`;
register("data:text/javascript," + encodeURIComponent(loader));
const { perform } = await import("../perform.js");
const { invocation, detailActions } = await import("../actions.js");
const { normaliseBoard } = await import("../model.js");

const board = normaliseBoard(JSON.parse(readFileSync(new URL("../fixtures/board.json", import.meta.url), "utf8")));
// The fixture's running build in terminal t7 on studio.
const inTerminal = board.cards.find((c) => c.activity?.terminal?.address === "t7");

test("Watch calls agent.focus with the card's terminal", async () => {
  assert.ok(inTerminal, "the fixture has a card whose agent runs in a terminal");
  assert.ok(detailActions(inTerminal, board).includes("watch"));
  for (const [on, terminal] of [[null, "t7"], ["studio", "studio/t7"]]) {
    calls.length = 0;
    const r = await perform(invocation("watch", { ...inTerminal, on }));
    assert.deepEqual(r, { ok: true, message: "" });
    assert.deepEqual(calls, [{ command: "agent.focus", args: { terminal } }]);
  }
});

test("Watch with the fixtures only says what it would do", async () => {
  calls.length = 0;
  const r = await perform(invocation("watch", inTerminal), { dry: true });
  assert.equal(r.dry, true);
  assert.deepEqual(calls, []);
});
