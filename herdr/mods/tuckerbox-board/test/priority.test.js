// Priority on the board (board-data) and drag to reorder (herdr-board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard, group, sortedColumn, dropPosition, mergeBoards, eventBox, inBox, liveDrop, priorityCard } from "../model.js";
import { commandLine, invocation } from "../actions.js";

const fixture = JSON.parse(readFileSync(new URL("../fixtures/board.json", import.meta.url), "utf8"));
const col = (column, ...cards) => cards.map(([change, position]) => ({ change, column, position }));

test("cards carry priority and position; missing or bad values are none", () => {
  const board = normaliseBoard(fixture);
  const users = board.cards.find((c) => c.change === "add-users");
  assert.equal(users.priority, 1000);
  assert.equal(users.position, 1);
  const done = board.cards.find((c) => c.change === "add-sitemap");
  assert.equal(done.priority, null);
  assert.equal(done.position, 0);
  const odd = normaliseCard({ change: "x", priority: "high", position: -1 });
  assert.equal(odd.priority, null);
  assert.equal(odd.position, 0);
});

// Scenario: Board order. The board lists the cards of a sorted column in
// the document's order, which is priority order.
test("columns keep the document's priority order", () => {
  const board = normaliseBoard({ ...fixture, cards: [
    { change: "b", column: "approved", priority: 1000, position: 1 },
    { change: "a", column: "approved", priority: 2000, position: 2 },
  ] });
  const approved = group(board).find((c) => c.id === "approved");
  assert.deepEqual(approved.cards.map((c) => c.change), ["b", "a"]);
});

test("only Proposed, Approved, In progress, Queued and step columns are sorted", () => {
  for (const id of ["proposed", "approved", "in_progress", "queued", "step:build"]) assert.ok(sortedColumn(id), id);
  for (const id of ["ready", "done", "", undefined]) assert.ok(!sortedColumn(id), String(id));
});

// Scenario: Drag above a card. D at 5 dragged above B at 2 runs
// tuckerbox priority D 2.
test("drag above a card", () => {
  const cards = col("approved", ["b", 2], ["c", 3], ["d", 5]);
  assert.equal(dropPosition(cards, "d", "b"), 2);
  // From above: one less than the card it lands on.
  assert.equal(dropPosition(col("approved", ["a", 1], ["b", 2], ["c", 3]), "a", "c"), 2);
  // The lower half of a card means above the next one.
  assert.equal(dropPosition(cards, "d", "b", true), 3);
  const inv = invocation("priority", { change: "d", on: "studio", path: "/work/shop" }, { position: 2 });
  assert.deepEqual(inv.argv, ["tuckerbox", "priority", "d", "2"]);
  assert.equal(inv.place, "exec");
  assert.equal(inv.on, "studio");
});

// Scenario: Drag down. A at 1 dragged below C at 3 (the last card) runs
// tuckerbox priority A 3.
test("drag down below the last card", () => {
  const cards = col("approved", ["a", 1], ["b", 2], ["c", 3]);
  assert.equal(dropPosition(cards, "a", "c", true), 3);
  // From below the last card's position: one more.
  assert.equal(dropPosition(col("queued", ["x", 4], ["y", 2]), "x", "y", true), 3);
});

test("a drop that moves nothing, or can't reorder, runs nothing", () => {
  const cards = col("approved", ["a", 1], ["b", 2]);
  assert.equal(dropPosition(cards, "a", "a"), null);
  assert.equal(dropPosition(cards, "a", "b"), null); // above b, from above: stays 1
  assert.equal(dropPosition(col("ready", ["a", 1], ["b", 2]), "b", "a"), null);
  assert.equal(dropPosition(cards, "a", "nope"), null);
  assert.equal(dropPosition(col("approved", ["a", 0], ["b", 2]), "a", "b", true), null);
});

test("the priority command line takes a position from 1 only", () => {
  assert.deepEqual(commandLine("priority", "add-login", { position: 7 }), ["tuckerbox", "priority", "add-login", "7"]);
  for (const position of [0, -1, 1.5, "2", undefined]) assert.throws(() => commandLine("priority", "add-login", { position }));
});

// A small drag that ends on the dragged card itself, on either half, moves
// nothing, even with gaps between the column's positions.
test("a drop on the dragged card itself runs nothing", () => {
  const cards = col("approved", ["a", 1], ["b", 4], ["c", 7]);
  assert.equal(dropPosition(cards, "b", "b", false), null);
  assert.equal(dropPosition(cards, "b", "b", true), null);
  const last = col("approved", ["a", 1], ["b", 4]);
  assert.equal(dropPosition(last, "b", "b", false), null);
  assert.equal(dropPosition(last, "b", "b", true), null);
  // A real move in the same column still works.
  assert.equal(dropPosition(cards, "b", "a"), 1);
});

test("a drop target holds only while the pointer is over its card", () => {
  const box = eventBox({ position: { x: 150, y: 260 }, local_position: { x: 50, y: 60 }, bounds: { width: 200, height: 100 } });
  assert.deepEqual(box, { x: 100, y: 200, w: 200, h: 100 });
  assert.ok(inBox(box, { x: 100, y: 200 }));
  assert.ok(inBox(box, { x: 300, y: 300 }));
  assert.ok(!inBox(box, { x: 301, y: 250 }));
  const drop = { over: "b", below: false, box };
  assert.equal(liveDrop(drop, { x: 150, y: 250 }), drop);
  // Released in another column or off the board: cancelled.
  assert.equal(liveDrop(drop, { x: 600, y: 250 }), null);
  assert.equal(liveDrop(drop, { x: 150, y: 50 }), null);
  assert.equal(liveDrop(drop, undefined), null);
  assert.equal(liveDrop(null, { x: 150, y: 250 }), null);
});

// Priority is the board machine's: a merged card keeps the own copy's
// priority and position, and the move runs on the board's machine.
test("merged boards keep the board machine's priority order", () => {
  const doc = (machine, path, cards) => ({ version: 1, repo: "github.com/owner/shop", path, machine, require_approval: true, steps: ["build"], columns: fixture.columns, cards });
  const c = (change, column, priority, position, extra = {}) => ({ change, column, priority, position, tasks: { done: 0, total: 1 }, approval: { state: "approved" }, ...extra });
  const own = normaliseBoard(doc("studio", "/work/shop", [
    c("a", "approved", 1000, 1), c("b", "approved", 2000, 2), c("c", "approved", 3000, 3),
  ]), { on: null });
  // mini's checkout has its own (stale) priorities, and runs b.
  const mini = normaliseBoard(doc("mini", "/Users/sam/shop", [
    c("a", "approved", 3000, 3), c("b", "approved", 1000, 1, { state: "queued" }), c("c", "approved", 2000, 2), c("z", "approved", 500, 1),
  ]), { on: "mini" });
  const b = mergeBoards(own, [{ machine: "mini", board: mini }]);
  const by = Object.fromEntries(b.cards.map((x) => [x.change, x]));
  assert.equal(by.b.on, "mini"); // the run's copy wins for state
  assert.equal(by.b.priority, 2000);
  assert.equal(by.b.position, 2);
  // A change only mini has gets no position here: it can't be dragged.
  assert.equal(by.z.priority, null);
  assert.equal(by.z.position, 0);
  // Sorted by the board machine's order, not mini's.
  assert.deepEqual(group(b).find((x) => x.id === "approved").cards.map((x) => x.change), ["a", "b", "c", "z"]);
  // The move runs on the board's machine, in its checkout.
  const moved = priorityCard(by.b, b);
  assert.equal(moved.on, null);
  assert.equal(moved.path, "/work/shop");
  assert.equal(moved.change, "b");
});

test("sorted columns list cards by position, unpositioned last", () => {
  const board = normaliseBoard({ ...fixture, cards: [
    { change: "x", column: "approved" }, { change: "c", column: "approved", position: 3 },
    { change: "a", column: "approved", position: 1 }, { change: "d", column: "done" }, { change: "e", column: "done" },
  ] });
  const g = group(board);
  assert.deepEqual(g.find((x) => x.id === "approved").cards.map((x) => x.change), ["a", "c", "x"]);
  assert.deepEqual(g.find((x) => x.id === "done").cards.map((x) => x.change), ["d", "e"]);
});
