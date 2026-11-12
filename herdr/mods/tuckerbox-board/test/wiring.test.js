// How actions reach Herdr and what the board shows from their results
// (herdr-board scenarios 6.4 to 6.6).   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard } from "../model.js";
import { invocation, herdrCall, outcome, parseValidation } from "../actions.js";
import { startEdit, tick, editOf, editNote, setValidation, markValidating, keyCard, editKey, reset, OPEN_MS, machineOfPane } from "../edits.js";

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

// The board's own space s1 here; a remote space on mini.
const SPACES = [
  { id: "s1", machine: null, tabs: [{ id: "t1" }, { id: "t2" }] },
  { id: "s9", machine: null, tabs: [{ id: "t9" }] },
  { id: "mini/s2", machine: "mini", tabs: [{ id: "mini/t5" }] },
];
const here = (c) => ({ on: null, space: "s1", folder: c.path });

test("Edit leaves the change invalid: the pane closes, validate runs, the card shows the error", () => {
  const c = card("add-login-page");
  const before = [{ pane: "p1", terminal: "t1", tab: "t1", cwd: "/work/shop" }, { pane: "p2", mod: MOD, terminal: null, tab: "t1" }];
  startEdit(c, "spec:login", before, 1000, here(c));
  assert.equal(editOf(c).state, "opening");
  assert.match(editNote(editOf(c)).text, /Opening the editor/);
  // Its pane appears, in the space and folder: claimed.
  const during = [...before, { pane: "p9", terminal: "t9", tab: "t1", cwd: "/work/shop" }];
  assert.deepEqual(tick(during, 1500, SPACES), []);
  assert.equal(editOf(c).state, "editing");
  assert.equal(editOf(c).pane, "p9");
  assert.equal(editNote(editOf(c)).validate, false);
  assert.deepEqual(tick(during, 60000, SPACES), []);
  // It closes: once, and only once, the card is handed back to validate.
  const closed = tick(before, 61000, SPACES);
  assert.deepEqual(closed.map((k) => keyCard(k).change), ["add-login-page"]);
  assert.deepEqual(keyCard(closed[0]), { on: null, path: "/work/shop", change: "add-login-page" });
  assert.equal(editOf(c).state, "validating");
  assert.deepEqual(tick(before, 62000, SPACES), []);
  setValidation(c, { valid: false, errors: ["specs/login/spec.md: Requirement must have at least one scenario"] });
  const n = editNote(editOf(c));
  assert.equal(n.tone, "blocked");
  assert.match(n.text, /Invalid after the last edit:\nspecs\/login\/spec.md: Requirement must have at least one scenario/);
  assert.equal(n.validate, true);
});

test("an unrelated new terminal isn't taken for the editor", () => {
  const c = card("add-login-page");
  startEdit(c, "tasks", [], 0, here(c));
  // Another space, another folder, another machine, a mod pane: none is it.
  tick([
    { pane: "p20", terminal: "t20", tab: "t9", cwd: "/work/shop" },
    { pane: "p21", terminal: "t21", tab: "t2", cwd: "/home/me" },
    { pane: "mini/p22", terminal: "mini/t22", tab: "mini/t5", cwd: "/work/shop" },
    { pane: "p23", mod: MOD, terminal: null, tab: "t1" },
    { pane: "p24", terminal: "t24", tab: "t1", cwd: null },
  ], 100, SPACES);
  assert.equal(editOf(c).state, "opening");
  // The real editor then appears and is claimed; closing p21 changes nothing.
  const now = [{ pane: "p21", terminal: "t21", tab: "t2", cwd: "/home/me" }, { pane: "p25", terminal: "t25", tab: "t2", cwd: "/work/shop/openspec" }];
  tick(now, 200, SPACES);
  assert.equal(editOf(c).pane, "p25");
  assert.deepEqual(tick([now[1]], 300, SPACES), [], "the unrelated pane closing doesn't validate");
  assert.equal(tick([], 400, SPACES).length, 1, "the editor's pane closing does");
});

test("a remote edit claims only a pane on its machine, in its folder", () => {
  const c = card("add-login-page", "mini");
  // space.new on mini: the space isn't known beforehand.
  startEdit(c, "proposal", [], 0, { on: "mini", space: null, folder: "/srv/shop" });
  tick([{ pane: "p30", terminal: "t30", tab: "t1", cwd: "/srv/shop" }], 50, SPACES);
  assert.equal(editOf(c).state, "opening", "a local pane in the same path isn't it");
  tick([{ pane: "p30", terminal: "t30", tab: "t1", cwd: "/srv/shop" }, { pane: "mini/p31", terminal: "mini/t31", tab: "mini/t6", cwd: "/srv/shop" }], 60, SPACES);
  assert.equal(editOf(c).pane, "mini/p31");
});

test("two edits at once: each claims its own pane, or both fall back to Validate", () => {
  const a = card("add-login-page");
  const b = { ...card("order-emails"), path: "/work/other" };
  startEdit(a, "tasks", [], 0, here(a));
  startEdit(b, "design", [], 0, { on: null, space: "s1", folder: "/work/other" });
  // One pane per folder: each gets its own.
  const panes = [{ pane: "p40", terminal: "t40", tab: "t1", cwd: "/work/other" }, { pane: "p41", terminal: "t41", tab: "t1", cwd: "/work/shop" }];
  tick(panes, 10, SPACES);
  assert.equal(editOf(a).pane, "p41");
  assert.equal(editOf(b).pane, "p40");
  // Closing one validates only that edit.
  assert.deepEqual(tick([panes[1]], 20, SPACES).map((k) => keyCard(k).change), ["order-emails"]);
  assert.equal(editOf(a).state, "editing");

  // Two edits in the same checkout, one new pane: it could be either's.
  reset();
  const c = card("add-login-page");
  const d = card("add-wishlist");
  startEdit(c, "tasks", [], 0, here(c));
  startEdit(d, "tasks", [], 0, here(d));
  tick([{ pane: "p50", terminal: "t50", tab: "t1", cwd: "/work/shop" }], 10, SPACES);
  for (const x of [c, d]) {
    assert.equal(editOf(x).state, "unwatched", x.change);
    assert.equal(editOf(x).pane, null);
    assert.match(editNote(editOf(x)).text, /couldn't tell which pane is the editor/);
    assert.equal(editNote(editOf(x)).validate, true);
  }
  // A pane already claimed by one edit is never another's.
  reset();
  startEdit(c, "tasks", [], 0, here(c));
  tick([{ pane: "p60", terminal: "t60", tab: "t1", cwd: "/work/shop" }], 10, SPACES);
  startEdit(d, "tasks", [], 20, here(d)); // before-set empty: p60 looks new to it
  tick([{ pane: "p60", terminal: "t60", tab: "t1", cwd: "/work/shop" }], 30, SPACES);
  assert.equal(editOf(c).pane, "p60");
  assert.equal(editOf(d).state, "opening");
});

test("pane close can't be seen: not validated since the edit, Validate offered", () => {
  const c = card("order-emails");
  startEdit(c, "tasks", [], 0, here(c));
  tick([], OPEN_MS - 1, SPACES);
  assert.equal(editOf(c).state, "opening");
  tick([], OPEN_MS + 1, SPACES);
  const n = editNote(editOf(c));
  assert.equal(editOf(c).state, "unwatched");
  assert.equal(n.validate, true);
  assert.match(n.text, /couldn't see the editor's pane/);
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
  startEdit(a, "tasks", [], 0, here(a));
  assert.equal(editOf(b), null);
  assert.notEqual(editKey(a), editKey(b));
});

test("pane ids name their machine", () => {
  assert.equal(machineOfPane("p3"), null);
  assert.equal(machineOfPane("mini/p3"), "mini");
});
