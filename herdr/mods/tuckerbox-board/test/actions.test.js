// Which actions each card state offers, and the tuckerbox command behind each
// (herdr-board: Actions go through Tuckerbox, Run actions, Editing from the
// board, Tasks and approval from the board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard } from "../model.js";
import { commandLine, invocation, detailActions, cardActions, canEdit, defaultStep, actionLabel, shown } from "../actions.js";

const fixture = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const board = normaliseBoard(fixture("board.json"), { on: null });
const card = (id) => board.cards.find((c) => c.change === id);
const offOff = { ...board, require_approval: false };

test("every action builds its tuckerbox command line", () => {
  const c = "add-login-page";
  assert.deepEqual(commandLine("start", c), ["tuckerbox", "run", c]);
  assert.deepEqual(commandLine("approve", c), ["tuckerbox", "approve", c]);
  assert.deepEqual(commandLine("unapprove", c), ["tuckerbox", "unapprove", c]);
  assert.deepEqual(commandLine("stop", c), ["tuckerbox", "stop", c]);
  assert.deepEqual(commandLine("resume", c), ["tuckerbox", "resume", c]);
  assert.deepEqual(commandLine("rerun", c, { step: "review" }), ["tuckerbox", "rerun", c, "review"]);
  assert.deepEqual(commandLine("follow", c, { step: "build" }), ["tuckerbox", "logs", c, "build", "--follow"]);
  assert.deepEqual(commandLine("edit", c, { file: "proposal" }), ["tuckerbox", "edit", c, "proposal"]);
  assert.deepEqual(commandLine("edit", c, { file: "spec:login" }), ["tuckerbox", "edit", c, "spec:login"]);
  assert.deepEqual(commandLine("validate", c), ["tuckerbox", "validate", c, "--json"]);
  assert.deepEqual(commandLine("tick", c, { task: "2.1" }), ["tuckerbox", "tasks", c, "tick", "2.1"]);
  assert.deepEqual(commandLine("untick", c, { task: "2.1" }), ["tuckerbox", "tasks", c, "untick", "2.1"]);
  assert.deepEqual(commandLine("init", ""), ["tuckerbox", "init"]);
  assert.equal(commandLine("proposal", c), null);
  assert.equal(commandLine("worktree", c), null);
});

test("a command never gets input it must not run", () => {
  assert.throws(() => commandLine("start", "--all"));
  assert.throws(() => commandLine("start", "x; rm -rf ~"));
  assert.throws(() => commandLine("start", ""));
  assert.throws(() => commandLine("tick", "a", { task: "2" }));
  assert.throws(() => commandLine("tick", "a", { task: "2.1 --x" }));
  assert.throws(() => commandLine("edit", "a", { file: "../etc/passwd" }));
  assert.throws(() => commandLine("edit", "a", { file: "spec:" }));
  assert.throws(() => commandLine("rerun", "a", {}));
  assert.throws(() => commandLine("follow", "a", {}));
  assert.throws(() => commandLine("merge", "a"));
});

test("Start from the board: an Approved card runs tuckerbox run in the repository", () => {
  const c = card("add-wishlist");
  assert.equal(c.column, "approved");
  assert.ok(detailActions(c, board).includes("start"));
  assert.ok(cardActions(c, board).includes("start"));
  const inv = invocation("start", c);
  assert.deepEqual(inv.argv, ["tuckerbox", "run", "add-wishlist"]);
  assert.equal(inv.place, "exec");
  assert.equal(inv.cwd, "/work/shop");
  assert.equal(inv.on, null);
});

test("Start not offered before approval: a Proposed card offers Approve, not Start", () => {
  for (const id of ["add-search", "refund-flow"]) {
    const c = card(id);
    assert.equal(c.column, "proposed");
    assert.ok(!detailActions(c, board).includes("start"), id);
    assert.ok(!cardActions(c, board).includes("start"), id);
    assert.ok(detailActions(c, board).includes("approve"), id);
    assert.deepEqual(cardActions(c, board), ["approve"], id);
  }
});

test("approval not required: a Proposed card offers Start", () => {
  const c = card("add-search");
  assert.ok(detailActions(c, offOff).includes("start"));
  assert.ok(cardActions(c, offOff).includes("start"));
});

test("Follow a running step: a tab on the card's machine running tuckerbox logs --follow", () => {
  const c = { ...card("add-users"), on: "mini" };
  assert.equal(c.state, "running");
  assert.ok(detailActions(c, board).includes("follow"));
  const inv = invocation("follow", c, { step: c.step });
  assert.equal(inv.place, "tab");
  assert.equal(inv.on, "mini");
  assert.deepEqual(inv.argv, ["tuckerbox", "logs", "add-users", "build", "--follow"]);
  assert.equal(actionLabel("follow", { step: "build" }), "Follow build log ↗");
});

test("each state offers its actions", () => {
  const offers = (id) => detailActions(card(id), board);
  // queued and running: Stop; nothing that takes the run lock
  for (const id of ["checkout-v2", "add-users", "fix-tax-rounding"]) {
    const a = offers(id);
    assert.ok(a.includes("stop"), id);
    for (const no of ["start", "resume", "rerun", "approve", "unapprove"]) assert.ok(!a.includes(no), `${id} offers ${no}`);
    assert.equal(canEdit(card(id)), false, id);
  }
  assert.ok(!offers("checkout-v2").includes("follow"), "a queued run has no step log yet");
  // blocked and stopped: Resume and Re-run; failed: Re-run, not Resume
  for (const id of ["add-login-page", "order-emails"]) {
    assert.ok(offers(id).includes("resume"), id);
    assert.ok(offers(id).includes("rerun"), id);
    assert.ok(!offers(id).includes("stop"), id);
    assert.equal(canEdit(card(id)), true, id);
  }
  assert.ok(!offers("image-cdn").includes("resume"));
  assert.ok(offers("image-cdn").includes("rerun"));
  assert.deepEqual(cardActions(card("image-cdn"), board), ["rerun"]);
  // ready: Re-run and Open worktree, no Start
  assert.ok(offers("cart-badge").includes("rerun"));
  assert.ok(offers("cart-badge").includes("worktree"));
  assert.ok(!offers("cart-badge").includes("start"));
  assert.equal(defaultStep(card("cart-badge"), board), "plan");
  // archived: nothing
  assert.deepEqual(offers("add-sitemap"), []);
  assert.equal(canEdit(card("add-sitemap")), false);
});

test("Approve shows for none and stale, Unapprove for approved", () => {
  assert.ok(detailActions(card("add-search"), board).includes("approve"));
  assert.ok(detailActions(card("refund-flow"), board).includes("approve"));
  assert.ok(detailActions(card("order-emails"), board).includes("approve"), "stale on a stopped run");
  assert.ok(detailActions(card("add-wishlist"), board).includes("unapprove"));
  assert.ok(!detailActions(card("add-wishlist"), board).includes("approve"));
  assert.deepEqual(invocation("approve", card("add-search")).argv, ["tuckerbox", "approve", "add-search"]);
  assert.deepEqual(invocation("unapprove", card("add-wishlist")).argv, ["tuckerbox", "unapprove", "add-wishlist"]);
});

test("Tick from the board: a stopped card's task 2.1", () => {
  const c = card("order-emails");
  assert.equal(c.state, "stopped");
  assert.equal(canEdit(c), true);
  assert.deepEqual(invocation("tick", c, { task: "2.1" }).argv, ["tuckerbox", "tasks", "order-emails", "tick", "2.1"]);
});

test("Edit during a run: no Edit while building; Edit opens a pane on the card's machine", () => {
  assert.equal(canEdit(card("add-users")), false);
  const c = { ...card("add-login-page"), on: "mini" };
  const inv = invocation("edit", c, { file: "spec:login" });
  assert.equal(inv.place, "pane");
  assert.equal(inv.on, "mini");
  assert.deepEqual(inv.argv, ["tuckerbox", "edit", "add-login-page", "spec:login"]);
});

test("Open proposal and Open worktree are Herdr views with the card's paths", () => {
  const c = card("add-login-page");
  const p = invocation("proposal", c);
  assert.equal(p.place, "view");
  assert.equal(p.argv, null);
  assert.equal(p.file, "/work/shop-worktrees/add-login-page/openspec/changes/add-login-page/proposal.md");
  assert.equal(invocation("worktree", c).folder, "/work/shop-worktrees/add-login-page");
  assert.ok(!detailActions(card("add-search"), board).includes("worktree"), "no worktree before a run");
});

test("a card from another machine runs its commands there", () => {
  const c = normaliseCard({ change: "x", column: "approved", approval: { state: "approved" } }, { on: "mini", path: "/srv/shop" });
  const inv = invocation("start", c);
  assert.equal(inv.on, "mini");
  assert.equal(inv.cwd, "/srv/shop");
});

test("shown quotes only what needs it", () => {
  assert.equal(shown(["tuckerbox", "run", "add-login-page"]), "tuckerbox run add-login-page");
  assert.equal(shown(["tuckerbox", "edit", "a", "spec:login"]), "tuckerbox edit a spec:login");
  assert.equal(shown(["echo", "it's"]), "echo 'it'\\''s'");
});
