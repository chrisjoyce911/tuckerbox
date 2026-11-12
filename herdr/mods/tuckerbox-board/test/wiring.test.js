// How actions reach Herdr and what the board shows from their results
// (herdr-board scenarios 6.4 to 6.6).   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard } from "../model.js";
import { invocation, herdrCall, outcome, parseValidation } from "../actions.js";
import { startEdit, step, tick, editOf, editNote, setValidation, markValidating, keyCard, editKey, reset, OPEN_MS } from "../edits.js";

const board = normaliseBoard(JSON.parse(readFileSync(new URL("../fixtures/board.json", import.meta.url), "utf8")));
const card = (id, on = null) => ({ ...board.cards.find((c) => c.change === id), on });
const MOD = "tuckerbox-board";

beforeEach(() => reset());

test("Follow a running step on mini: a tab there", () => {
  const c = card("add-users", "mini");
  const inv = invocation("follow", c, { step: "build" });
  // The shown space is on mini: a tab in it.
  const there = herdrCall(inv, { shown: { id: "mini/s4", machine: "mini" }, mod: MOD });
  assert.deepEqual(there, { command: "tab.new", args: { name: "add-users build log", space: "mini/s4", folder: "/work/shop", argv: ["tuckerbox", "logs", "add-users", "build", "--follow"] } });
  // The shown space is here: a new space on mini, since tabs stay on their space's machine.
  const here = herdrCall(inv, { shown: { id: "s1", machine: null }, mod: MOD });
  assert.equal(here.command, "space.new");
  assert.equal(here.args.machine, "mini");
  assert.deepEqual(here.args.argv, ["tuckerbox", "logs", "add-users", "build", "--follow"]);
});

test("Edit: a pane on the card's machine running tuckerbox edit, in its checkout", () => {
  const inv = invocation("edit", card("add-login-page"), { file: "spec:login" });
  const call = herdrCall(inv, { shown: { id: "s1", machine: null }, mod: MOD });
  assert.deepEqual(call, { command: "terminal.open", args: { argv: ["tuckerbox", "edit", "add-login-page", "spec:login"], folder: "/work/shop", place: "right", name: "edit add-login-page spec:login" } });
  const remote = herdrCall(invocation("edit", card("add-login-page", "mini"), { file: "tasks" }), { shown: { id: "s1", machine: null }, mod: MOD });
  assert.equal(remote.command, "space.new");
  assert.equal(remote.args.machine, "mini");
});

test("Open proposal: this mod's proposal view in a pane beside the board", () => {
  const c = card("add-login-page");
  const inv = { ...invocation("proposal", c), card: c, board: { path: "/work/shop" } };
  const call = herdrCall(inv, { shown: { id: "s1", machine: null }, mod: MOD });
  assert.equal(call.command, "pane.split");
  assert.equal(call.args.direction, "right");
  assert.equal(call.args.mod, MOD);
  assert.equal(call.args.props.view, "proposal");
  assert.equal(call.args.props.change, "add-login-page");
});

test("Open worktree: the space already there, else a new one on the card's machine", () => {
  const c = card("add-login-page", "mini");
  const inv = invocation("worktree", c);
  const spaces = [
    { id: "s1", machine: null, folder: "/work/shop-worktrees/add-login-page", tabs: [] },
    { id: "mini/s2", machine: "mini", folder: "/home/me", tabs: [{ id: "mini/t1" }] },
  ];
  // Same path on another machine doesn't count; a terminal of mini/s2 inside the worktree does.
  const panes = [{ pane: "mini/p1", tab: "mini/t1", cwd: "/work/shop-worktrees/add-login-page/web" }];
  assert.deepEqual(herdrCall(inv, { spaces, panes, mod: MOD }), { command: "space.select", args: { id: "mini/s2" } });
  assert.deepEqual(herdrCall(inv, { spaces, panes: [], mod: MOD }), { command: "space.new", args: { name: "add-login-page", folder: "/work/shop-worktrees/add-login-page", machine: "mini" } });
  assert.deepEqual(herdrCall(invocation("worktree", card("add-login-page")), { spaces, panes: [], mod: MOD }), { command: "space.select", args: { id: "s1" } });
});

test("exec actions are not Herdr calls", () => {
  assert.throws(() => herdrCall(invocation("start", card("add-wishlist")), {}));
});

test("Action fails: the board shows the command's message", () => {
  const r = outcome({ code: 1, stdout: "", stderr: "tuckerbox run: add-wishlist: approval none on origin/main (the checkout's approval is not committed)\n" });
  assert.equal(r.ok, false);
  assert.match(r.message, /approval none on origin\/main/);
  assert.equal(outcome({ code: -1, stdout: "", stderr: "exec: tuckerbox: not found" }).message, "exec: tuckerbox: not found");
  assert.equal(outcome({ code: 3, stdout: "", stderr: "" }).message, "exited with code 3");
});

test("Approve from the board: the note that nothing was committed is shown", () => {
  const r = outcome({ code: 0, stdout: "approved add-search in the checkout (by Sam at 2026-11-12T09:00:00+10:00, sha256:ab)\nnot committed: commit and push openspec/changes/add-search/tuckerbox.toml\n", stderr: "" });
  assert.equal(r.ok, true);
  assert.match(r.message, /not committed: commit and push/);
});

test("Approve an invalid change: the validation errors are shown", () => {
  const r = outcome({ code: 1, stdout: "", stderr: "✗ add-search: specs/x/spec.md: requirement has no scenario\ntuckerbox approve: add-search is not valid; not approved\n" });
  assert.equal(r.ok, false);
  assert.match(r.message, /requirement has no scenario/);
});

test("validate --json: invalid still prints the document", () => {
  const out = JSON.stringify({ version: 1, change: "a", folder: "/w", valid: false, errors: [{ level: "ERROR", path: "specs/login/spec.md", message: "Requirement must have at least one scenario" }] });
  assert.deepEqual(parseValidation({ code: 1, stdout: out, stderr: "" }), { valid: false, errors: ["specs/login/spec.md: Requirement must have at least one scenario"] });
  assert.deepEqual(parseValidation({ code: 0, stdout: JSON.stringify({ version: 1, valid: true, errors: [] }), stderr: "" }), { valid: true, errors: [] });
  assert.deepEqual(parseValidation({ code: 1, stdout: "", stderr: "tuckerbox validate: no change x" }), { error: "tuckerbox validate: no change x" });
});

test("Edit leaves the change invalid: the pane closes, validate runs, the card shows the error", () => {
  const c = card("add-login-page");
  const before = [{ pane: "p1", terminal: "t1" }, { pane: "p2", mod: MOD, terminal: null }];
  startEdit(c, "spec:login", before, 1000);
  assert.equal(editOf(c).state, "opening");
  assert.match(editNote(editOf(c)).text, /Opening the editor/);
  // The pane appears.
  const during = [...before, { pane: "p9", terminal: "t9" }];
  assert.deepEqual(tick(during, 1500), []);
  assert.equal(editOf(c).state, "editing");
  assert.equal(editOf(c).pane, "p9");
  assert.equal(editNote(editOf(c)).validate, false);
  // Still open: nothing.
  assert.deepEqual(tick(during, 60000), []);
  // It closes: once, and only once, the card is handed back to validate.
  const closed = tick(before, 61000);
  assert.deepEqual(closed.map((k) => keyCard(k).change), ["add-login-page"]);
  assert.deepEqual(keyCard(closed[0]), { on: null, path: "/work/shop", change: "add-login-page" });
  assert.equal(editOf(c).state, "validating");
  assert.deepEqual(tick(before, 62000), []);
  setValidation(c, { valid: false, errors: ["specs/login/spec.md: Requirement must have at least one scenario"] });
  const n = editNote(editOf(c));
  assert.equal(n.tone, "blocked");
  assert.match(n.text, /Invalid after the last edit:\nspecs\/login\/spec.md: Requirement must have at least one scenario/);
  assert.equal(n.validate, true);
});

test("pane close can't be seen: not validated since the edit, Validate offered", () => {
  const c = card("order-emails");
  startEdit(c, "tasks", [], 0);
  assert.equal(step(editOf(c), [], OPEN_MS - 1).state, "opening");
  tick([], OPEN_MS + 1);
  const n = editNote(editOf(c));
  assert.equal(editOf(c).state, "unwatched");
  assert.equal(n.validate, true);
  assert.match(n.text, /Not validated since the edit/);
  markValidating(c);
  assert.equal(editOf(c).state, "validating");
  setValidation(c, { valid: true, errors: [] });
  assert.equal(editNote(editOf(c)).text, "Valid after the last edit");
  setValidation(c, { error: "tuckerbox validate: exec failed" });
  assert.match(editNote(editOf(c)).text, /Validation didn't run/);
});

test("edits are kept per card: machine, checkout and change", () => {
  const a = card("add-login-page");
  const b = card("add-login-page", "mini");
  startEdit(a, "tasks", [], 0);
  assert.equal(editOf(b), null);
  assert.notEqual(editKey(a), editKey(b));
  // A mod pane appearing (the proposal view) is not the editor's pane.
  tick([{ pane: "p5", mod: MOD, terminal: null }], 10);
  assert.equal(editOf(a).state, "opening");
});
