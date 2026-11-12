// The board's drag handling (herdr-board: Drag to reorder), with the
// real model and a stand-in for gpui and the action runner.   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import * as model from "../model.js";

globalThis.__model = model;
const source = readFileSync(new URL("../board.js", import.meta.url), "utf8");
const stubs = `
class View {}
const { group, sortedColumn, dropPosition, eventBox, liveDrop, priorityCard } = globalThis.__model;
`;
const { BoardView } = await import(`data:text/javascript;base64,${Buffer.from(stubs + source.replace(/^import .*;\n/gm, "")).toString("base64")}`);

const doc = (machine, path, cards) => ({ version: 1, repo: "github.com/owner/shop", path, machine, require_approval: true, steps: ["build"],
  columns: [{ id: "approved", kind: "approved", name: "Approved" }, { id: "in_progress", kind: "in_progress", name: "In progress" }], cards });
const c = (change, column, position, extra = {}) => ({ change, column, priority: position * 1000, position, tasks: { done: 0, total: 1 }, approval: { state: "approved" }, ...extra });
const cx = { notify() {} };
// A card 200 wide and 100 high whose top-left is at (x, y) in the window.
const over = (x, y, ly) => ({ position: { x: x + 10, y: y + ly }, local_position: { x: 10, y: ly }, bounds: { width: 200, height: 100 } });

function view(board) {
  const v = new BoardView();
  v.board = board;
  v.acts = [];
  v.act = (action, card, extra) => v.acts.push({ action, card, extra });
  return v;
}

function start(v, card) {
  v.press = { change: card.change, column: card.column, x: 0, y: 0 };
  v.drag = card.change; v.dragColumn = card.column; v.drop = null;
}

test("a release over the target runs the move on the board's machine", () => {
  const own = model.normaliseBoard(doc("studio", "/work/shop", [c("a", "approved", 1), c("b", "approved", 2), c("d", "approved", 4)]), { on: null });
  const mini = model.normaliseBoard(doc("mini", "/Users/sam/shop", [c("d", "approved", 1, { state: "queued" })]), { on: "mini" });
  const board = model.mergeBoards(own, [{ machine: "mini", board: mini }]);
  const v = view(board);
  const d = board.cards.find((x) => x.change === "d");
  const a = board.cards.find((x) => x.change === "a");
  assert.equal(d.on, "mini");
  start(v, d);
  v.dragMove(over(0, 0, 20), cx, a);
  v.endDrag({ position: { x: 50, y: 30 } }, cx);
  assert.equal(v.acts.length, 1);
  assert.equal(v.acts[0].extra.position, 1);
  assert.equal(v.acts[0].card.on, null);
  assert.equal(v.acts[0].card.path, "/work/shop");
});

test("leaving the target, or releasing outside the column, cancels the drop", () => {
  const board = model.normaliseBoard(doc("studio", "/work/shop", [c("a", "approved", 1), c("b", "approved", 2), c("x", "in_progress", 3)]), { on: null });
  const [a, b, x] = board.cards;
  // Over a, then out into another column: the root sees the pointer leave.
  let v = view(board);
  start(v, b);
  v.dragMove(over(0, 0, 20), cx, a);
  assert.equal(v.drop?.over, "a");
  v.dragMove(over(300, 0, 20), cx, x); // a card of another column
  assert.equal(v.drop, null);
  v.endDrag({ position: { x: 310, y: 20 } }, cx);
  assert.deepEqual(v.acts, []);
  // Over a, then released elsewhere without a move event in between.
  v = view(board);
  start(v, b);
  v.dragMove(over(0, 0, 20), cx, a);
  v.endDrag({ position: { x: 600, y: 20 } }, cx);
  assert.deepEqual(v.acts, []);
  // Off the board (no position at all): cancelled too.
  v = view(board);
  start(v, b);
  v.dragMove(over(0, 0, 20), cx, a);
  v.dragMove({ position: { x: 50, y: 500 } }, cx, null);
  assert.equal(v.drop, null);
  v.endDrag({}, cx);
  assert.deepEqual(v.acts, []);
});

test("a small drag on the dragged card itself runs nothing", () => {
  const board = model.normaliseBoard(doc("studio", "/work/shop", [c("a", "approved", 1), c("b", "approved", 4)]), { on: null });
  const b = board.cards[1];
  for (const ly of [20, 80]) {
    const v = view(board);
    start(v, b);
    v.dragMove(over(0, 110, ly), cx, b);
    v.endDrag({ position: { x: 10, y: 110 + ly } }, cx);
    assert.deepEqual(v.acts, [], `half at ${ly}`);
  }
});
