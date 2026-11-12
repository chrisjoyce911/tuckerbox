// Runs from every machine (herdr-board): which machines are asked and how
// their boards merge into the one shown.   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { normaliseBoard, otherMachines, sharedRepo, fitColumn, mergeBoards, group, cardAt } from "../model.js";

const COMPACT = [
  { id: "proposed", kind: "proposed", name: "Proposed" },
  { id: "approved", kind: "approved", name: "Approved" },
  { id: "in_progress", kind: "in_progress", name: "In progress" },
  { id: "ready", kind: "ready", name: "Ready" },
  { id: "done", kind: "done", name: "Done" },
];

function doc(machine, path, cards, columns = COMPACT) {
  return { version: 1, repo: "github.com/owner/shop", path, machine, require_approval: true, steps: ["build", "review"], columns, cards };
}
const card = (change, extra = {}) => ({ change, column: "proposed", tasks: { done: 0, total: 2 }, approval: { state: "none" }, ...extra });

test("Two machines: one board shows both cards, each labelled with its machine", () => {
  const studio = normaliseBoard(doc("studio", "/work/shop", [
    card("change-a", { column: "in_progress", state: "running", step: "build", badge: "build", machine: "studio" }),
    card("change-b", { column: "approved", approval: { state: "approved" }, machine: "studio" }),
  ]), { on: null });
  const mini = normaliseBoard(doc("mini", "/Users/sam/shop", [
    card("change-a", { column: "approved", approval: { state: "approved" }, machine: "mini" }),
    card("change-b", { column: "in_progress", state: "running", step: "build", badge: "build", machine: "mini" }),
  ]), { on: "mini" });
  const b = mergeBoards(studio, [{ machine: "mini", board: mini }]);
  assert.equal(b.cards.length, 2);
  const a = b.cards.find((c) => c.change === "change-a");
  const bb = b.cards.find((c) => c.change === "change-b");
  assert.equal(a.machine, "studio");
  assert.equal(a.on, null);
  assert.equal(bb.machine, "mini");
  assert.equal(bb.state, "running");
  // Its actions run on mini, in mini's checkout.
  assert.equal(bb.on, "mini");
  assert.equal(bb.path, "/Users/sam/shop");
  assert.deepEqual(b.unreachable, []);
  assert.deepEqual(group(b).find((c) => c.id === "in_progress").cards.map((c) => c.change), ["change-a", "change-b"]);
});

test("An unreachable machine is named and hides no card", () => {
  const own = normaliseBoard(doc("studio", "/work/shop", [card("change-a")]));
  const b = mergeBoards(own, [
    { machine: "mini", board: { error: { code: "unreachable", message: "connection refused" } } },
    { machine: "laptop", board: { error: { code: "repo_not_found", message: "no checkout" } } },
  ]);
  assert.deepEqual(b.unreachable, [{ machine: "mini", message: "connection refused" }]);
  assert.deepEqual(b.cards.map((c) => c.change), ["change-a"]);
});

test("Merging prefers an active run, then any run, then the board's own copy", () => {
  const own = normaliseBoard(doc("studio", "/s", [
    card("idle"), card("blocked", { state: "blocked", column: "in_progress" }), card("both", { state: "blocked", column: "in_progress" }),
  ]));
  const other = normaliseBoard(doc("mini", "/m", [
    card("idle"), card("blocked"), card("both", { state: "queued", column: "in_progress" }), card("only-mini"),
  ]), { on: "mini" });
  const b = mergeBoards(own, [{ machine: "mini", board: other }]);
  const by = Object.fromEntries(b.cards.map((c) => [c.change, c]));
  assert.equal(by.idle.on, null);
  assert.equal(by.blocked.on, null);
  assert.equal(by.both.on, "mini");
  assert.equal(by["only-mini"].on, "mini");
});

test("A board for another repository is ignored", () => {
  const own = normaliseBoard(doc("studio", "/s", [card("a")]));
  const other = normaliseBoard({ ...doc("mini", "/m", [card("b")]), repo: "github.com/owner/other" }, { on: "mini" });
  assert.deepEqual(mergeBoards(own, [{ machine: "mini", board: other }]).cards.map((c) => c.change), ["a"]);
});

test("Cards from a machine with the other column layout fit the board shown", () => {
  const steps = [{ id: "queued" }, { id: "step:build" }, { id: "step:review" }, { id: "ready" }];
  assert.equal(fitColumn({ column: "step:build", step: "build" }, COMPACT), "in_progress");
  assert.equal(fitColumn({ column: "queued", step: "" }, COMPACT), "in_progress");
  assert.equal(fitColumn({ column: "in_progress", step: "review" }, steps), "step:review");
  assert.equal(fitColumn({ column: "in_progress", step: "" }, steps), "queued");
  assert.equal(fitColumn({ column: "ready", step: "" }, steps), "ready");
});

test("A queued card that keeps its step goes to Queued, not the step column", () => {
  const steps = [{ id: "queued" }, { id: "step:build" }, { id: "step:review" }, { id: "ready" }];
  assert.equal(fitColumn({ column: "in_progress", step: "build", state: "queued", badge: "queued" }, steps), "queued");
  assert.equal(fitColumn({ column: "in_progress", step: "build", state: "", badge: "queued" }, steps), "queued");
  assert.equal(fitColumn({ column: "in_progress", step: "build", state: "running", badge: "running" }, steps), "step:build");
});

test("A local card on a board opened from another machine runs on this computer", () => {
  // The board was opened in a space on mini; its own cards say on: "mini".
  const own = normaliseBoard(doc("mini", "/m", [card("a")]), { on: "mini" });
  const local = normaliseBoard(doc("studio", "/s", [card("b")]), { on: null });
  const b = mergeBoards(own, [{ machine: null, board: local }]);
  const lc = b.cards.find((c) => c.change === "b");
  assert.equal(lc.on, null);
  assert.deepEqual(cardAt(lc, b), { cwd: "/s", on: null });
  assert.deepEqual(cardAt(b.cards.find((c) => c.change === "a"), b), { cwd: "/m", on: "mini" });
  // Only a card without any machine field falls back to the board's.
  assert.deepEqual(cardAt({ change: "c", path: "" }, b), { cwd: "/m", on: "mini" });
  assert.deepEqual(cardAt(null, { path: "/x", on: null }), { cwd: "/x", on: null });
});

test("Which machines are asked", () => {
  const list = [{ name: null, layout: "ready" }, { name: "mini", layout: "ready" }, { name: "old", layout: "unavailable" }, { name: "new", layout: "loading" }];
  assert.deepEqual(otherMachines(list, null), [
    { name: "mini", reachable: true }, { name: "old", reachable: false }, { name: "new", reachable: true },
  ]);
  assert.deepEqual(otherMachines(list, "mini").map((m) => m.name), [null, "old", "new"]);
  assert.deepEqual(otherMachines(undefined), []);
});

test("A repository without a remote is not looked for elsewhere", () => {
  assert.equal(sharedRepo(normaliseBoard(doc("studio", "/s", []))), true);
  assert.equal(sharedRepo(normaliseBoard({ ...doc("studio", "/s", []), repo: "/s" })), false);
  assert.equal(sharedRepo({ error: { code: "x" } }), false);
});
