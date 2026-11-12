// Run group… and the task scope on cards (add-task-scope: Run group from
// the board, Scope on the board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard, normaliseDetail, eventText } from "../model.js";
import { commandLine, invocation, runGroups, outcome } from "../actions.js";

const fixture = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const board = normaliseBoard(fixture("board.json"), { on: null });
const card = (id) => board.cards.find((c) => c.change === id);

const readyCard = (scope) => normaliseCard({
  change: "add-login", column: "ready", state: "ready", round: 3,
  approval: { state: "approved" }, scope,
}, { path: "/work/shop" });
const detailWith = (groups) => normaliseDetail({ change: "add-login", groups });

test("Run group builds tuckerbox run --tasks and never takes anything but a group number", () => {
  assert.deepEqual(commandLine("group", "add-login", { group: 2 }), ["tuckerbox", "run", "add-login", "--tasks", "2"]);
  assert.throws(() => commandLine("group", "add-login", {}));
  assert.throws(() => commandLine("group", "add-login", { group: "2.1" }));
  assert.throws(() => commandLine("group", "add-login", { group: "next" }));
  assert.throws(() => commandLine("group", "add-login", { group: "2 --all" }));
});

test("Run the next group from a Ready card: it lists group 2 and runs it in the checkout", () => {
  const c = readyCard({ spec: "1", tasks: ["1.1", "1.2", "1.3"] });
  assert.deepEqual(c.scope, { spec: "1", tasks: ["1.1", "1.2", "1.3"] });
  const d = detailWith([
    { number: 1, title: "Page", ticked: 3, total: 3 },
    { number: 2, title: "Sign in", ticked: 0, total: 2 },
  ]);
  const groups = runGroups(c, board, d);
  assert.deepEqual(groups, [{ number: 2, title: "Sign in", ticked: 0, total: 2 }]);
  const inv = invocation("group", c, { group: groups[0].number });
  assert.deepEqual(inv.argv, ["tuckerbox", "run", "add-login", "--tasks", "2"]);
  assert.equal(inv.place, "exec");
  assert.equal(inv.cwd, "/work/shop");
  assert.equal(outcome({ code: 0, stdout: "add-login: started (pid 1, queued)" }).ok, true);
});

test("Nothing left to run: a Ready card whose groups are all ticked offers no Run group", () => {
  const c = readyCard({ spec: "2", tasks: ["2.1", "2.2"] });
  const d = detailWith([
    { number: 1, title: "Page", ticked: 3, total: 3 },
    { number: 2, title: "Sign in", ticked: 2, total: 2 },
    // Only a ship task left: counted out, so nothing to run.
    { number: 3, title: "Wrap-up", ticked: 0, total: 0 },
  ]);
  assert.deepEqual(runGroups(c, board, d), []);
});

test("Run group refused: the command's message shows and the result is not ok", () => {
  const r = outcome({ code: 1, stderr: "tuckerbox run: change add-login: approval is stale in its worktree /w: its text changed" });
  assert.equal(r.ok, false);
  assert.match(r.message, /approval is stale/);
});

test("Run group is offered where Start is, and not on running or proposed cards", () => {
  const groups = [{ number: 1, title: "First", ticked: 0, total: 2 }];
  const d = detailWith(groups);
  // An approved card with no run offers Start, so Run group too.
  assert.equal(runGroups(card("add-wishlist"), board, d).length, 1);
  // A running card does not.
  const running = board.cards.find((c) => c.state === "running");
  assert.ok(running);
  assert.deepEqual(runGroups(running, board, d), []);
  // A proposed card with approval required offers neither.
  const proposed = board.cards.find((c) => c.column === "proposed" && !c.state);
  assert.ok(proposed);
  assert.deepEqual(runGroups(proposed, board, d), []);
  // Without detail there is nothing to list.
  assert.deepEqual(runGroups(card("add-wishlist"), board, null), []);
});

test("The scope shows on cards, rounds and history", () => {
  assert.deepEqual(card("cart-badge").scope, { spec: "2", tasks: ["2.1", "2.2"] });
  assert.equal(card("add-wishlist").scope, null);
  const d = normaliseDetail({
    rounds: [{ round: 4, scope: "2" }],
    history: [
      { event: "scope_started", round: 4, scope: "2", old_scope: "1" },
      { event: "ready", round: 4, scope: "2" },
      { event: "scope_started", round: 5, scope: "", old_scope: "2" },
    ],
  });
  assert.equal(d.rounds[0].scope, "2");
  assert.equal(eventText(d.history[0]), "Started again for tasks 2 (was 1)");
  assert.equal(eventText(d.history[1]), "Ready (tasks 2)");
  assert.equal(eventText(d.history[2]), "Started again for tasks all (was 2)");
  const detail = normaliseDetail(fixture("detail.json").detail);
  assert.deepEqual(detail.groups.map((g) => `${g.number} ${g.ticked}/${g.total}`), ["1 2/2", "2 1/2", "3 0/1"]);
});
