// Autopilot on the board: needs_you, toasts and the header
// (add-autopilot, autopilot-board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, needsYou, needsYouReason, newlyNeeding, toastCall, counts } from "../model.js";

const fixture = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const doc = (cards, autopilot) => normaliseBoard({ version: 1, repo: "r", path: "/r", autopilot, cards });
const card = (change, extra = {}) => ({ change, column: "ready", approval: { state: "approved" }, ...extra });

test("needs_you flags a card, gives the reason, and counts in the header", () => {
  const b = doc([
    card("next", { state: "ready", needs_you: "approve again to run the next group" }),
    card("blocked", { state: "blocked", reason: "review limit reached" }),
    card("fine", { state: "ready" }),
  ]);
  assert.deepEqual(b.cards.map(needsYou), [true, true, false]);
  assert.equal(needsYouReason(b.cards[0]), "approve again to run the next group");
  assert.equal(needsYouReason(b.cards[1]), "blocked: review limit reached");
  assert.equal(counts(b).needs, 2);
});

test("Run blocks: one toast naming the change and the reason", () => {
  const before = doc([card("add-login", { state: "running", column: "in_progress" })]);
  const after = doc([card("add-login", { state: "blocked", reason: "review limit reached", needs_you: "blocked: review limit reached" })]);
  const first = newlyNeeding(null, before);
  const second = newlyNeeding(first.seen, after);
  assert.deepEqual(second.toasts, [{ change: "add-login", reason: "blocked: review limit reached" }]);
  assert.deepEqual(toastCall(second.toasts[0]), { title: "add-login needs you", body: "blocked: review limit reached", level: "warning", timeout: 8000 });
  // Still blocked at the next read: no second toast.
  assert.deepEqual(newlyNeeding(second.seen, after).toasts, []);
});

test("Board opened later: no toast at the first read, and the header counts 2", () => {
  const b = doc([
    card("a", { state: "blocked", reason: "x" }),
    card("b", { state: "failed", reason: "y" }),
    card("c", { state: "running" }),
  ]);
  assert.deepEqual(newlyNeeding(null, b).toasts, []);
  assert.equal(counts(b).needs, 2);
});

test("Autopilot on and off in the document", () => {
  assert.deepEqual(normaliseBoard(fixture("board.json")).autopilot, { running: true, pid: 4242, machine: "mini", started: "2026-11-12T08:00:00+10:00", terminal: "tb-ap-3f2a9c1d0b4e" });
  assert.equal(normaliseBoard(fixture("detail.json")).autopilot.running, false);
  assert.equal(doc([]).autopilot.running, false);
});
