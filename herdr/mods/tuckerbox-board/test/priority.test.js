// Priority on the board and drag to reorder (change-priority).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard, group, sortedColumn, dropPosition } from "../model.js";
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
