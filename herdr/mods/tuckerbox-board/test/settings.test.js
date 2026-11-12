// A change's step settings in card detail and the pickers that change them
// through tuckerbox set (settings-board).   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { normaliseBoard, normaliseCard, normaliseDetail } from "../model.js";
import { commandLine, invocation, outcome } from "../actions.js";
import { settingsRows, choose, canPick, voidsApproval, REPO_DEFAULT } from "../settings.js";

const fixture = (name) => JSON.parse(readFileSync(new URL(`../fixtures/${name}`, import.meta.url), "utf8"));
const board = normaliseBoard(fixture("board.json"), { on: null });
const detail = normaliseBoard(fixture("detail.json"), { on: null }).detail;
const card = (id) => board.cards.find((c) => c.change === id);

const S = (value, source) => ({ value, source });
const stepSettings = (review = {}) => [
  { step: "build", agent: S("claude", "repo"), model: S("opus", "repo"), effort: S("", "default"), permissions: S("edit", "machine") },
  { step: "review", agent: S("claude", "repo"), model: S("opus", "repo"), effort: S("", "default"), permissions: S("", "default"), ...review },
];
const rawCard = (extra = {}) => ({
  change: "add-login", column: "step:review", state: "stopped", step: "review", round: 2,
  approval: { state: "approved", by: "Sam", at: "2026-11-12T09:00:00+10:00", committed: true }, settings: stepSettings(), ...extra,
});
const row = (c, step) => settingsRows(c, detail).find((r) => r.step === step);
const key = (c, step, k) => row(c, step).keys.find((x) => x.key === k);

test("the board document carries settings, machines and choices", () => {
  const c = card("add-users");
  assert.deepEqual(c.settings.map((s) => s.step), ["plan", "build", "review"]);
  assert.deepEqual(c.settings[1].agent, { value: "codex", source: "change" });
  assert.equal(c.settings[1].started, true);
  assert.deepEqual(c.machines, ["studio"]);
  assert.deepEqual(card("add-sitemap").settings, []);
  assert.deepEqual(detail.choices.agents, ["claude", "codex"]);
  assert.deepEqual(detail.choices.models.claude, ["opus", "sonnet"]);
  assert.deepEqual(detail.choices.voids_approval, ["agent", "permissions"]);
  // Missing in an older document: no settings, nothing to pick.
  assert.deepEqual(normaliseCard({ change: "x" }).settings, []);
  assert.deepEqual(normaliseDetail({}).choices, { agents: [], models: {}, efforts: [], permissions: [], voids_approval: [] });
});

test("Pick a model: sonnet for the review step of a stopped card runs tuckerbox set", () => {
  const c = normaliseCard(rawCard(), { on: null, path: "/work/shop" });
  assert.equal(canPick(c), true);
  // The picker holds the models allowed for the step's agent, and the repository default.
  const model = key(c, "review", "model");
  assert.deepEqual(model.options.map((o) => o.label), ["opus", "sonnet", REPO_DEFAULT]);
  assert.equal(model.options.at(-1).value, null);
  assert.equal(model.text, "opus");
  assert.equal(model.source, "repo");
  // A model never asks, even on an approved card.
  const r = choose(c, detail, "review", "model", "sonnet");
  assert.equal(r.confirm, "");
  const inv = invocation("set", c, r.extra);
  assert.deepEqual(inv.argv, ["tuckerbox", "set", "add-login", "step.review.model=sonnet"]);
  assert.equal(inv.place, "exec");
  assert.equal(inv.cwd, "/work/shop");
  // What set prints is shown: that nothing was committed.
  const shown = outcome({ code: 0, stdout: "set step.review.model=sonnet for add-login in the repository checkout\nnot committed: it counts on other machines, and for runs that start without a worktree, only once openspec/changes/add-login/tuckerbox.toml is committed and pushed\n" });
  assert.equal(shown.ok, true);
  assert.match(shown.message, /not committed/);
  // The next board read shows sonnet from change; picking it again does nothing.
  const next = normaliseCard(rawCard({ settings: stepSettings({ model: S("sonnet", "change") }) }));
  assert.equal(key(next, "review", "model").text, "sonnet");
  assert.equal(key(next, "review", "model").source, "change");
  assert.deepEqual(choose(next, detail, "review", "model", "sonnet"), { noop: true });
  // Repository default removes the change's value.
  const back = choose(next, detail, "review", "model", null);
  assert.deepEqual(invocation("set", next, back.extra).argv, ["tuckerbox", "set", "add-login", "--unset", "step.review.model"]);
  assert.deepEqual(choose(c, detail, "review", "model", null), { noop: true }, "nothing of the change's own to remove");
});

test("models follow the step's agent; keys the allow-list doesn't permit get no picker", () => {
  const c = normaliseCard(rawCard({ settings: stepSettings({ agent: S("codex", "change"), model: S("", "default") }) }));
  assert.deepEqual(key(c, "review", "model").options.map((o) => o.value), ["gpt-5.5", null]);
  assert.equal(key(c, "review", "model").text, "agent default");
  const none = { ...detail, choices: { agents: [], models: {}, efforts: ["high"], permissions: [], voids_approval: [] } };
  const rows = settingsRows(c, none);
  assert.deepEqual(rows[0].keys.map((k) => k.options && k.key), [null, null, "effort", null]);
});

test("No pickers while running: a card running its build step shows its settings without pickers", () => {
  const running = card("add-users");
  assert.equal(running.state, "running");
  assert.equal(canPick(running), false);
  const rows = settingsRows(running, detail);
  assert.deepEqual(rows.map((r) => r.step), ["plan", "build", "review"]);
  for (const r of rows) for (const k of r.keys) assert.equal(k.options, null, `${r.step} ${k.key}`);
  assert.equal(row(running, "build").started, true);
  assert.equal(key(running, "build", "agent").text, "codex");
  for (const id of ["checkout-v2", "fix-tax-rounding", "add-sitemap"]) {
    assert.ok(settingsRows(card(id), detail).every((r) => r.keys.every((k) => k.options === null)), id);
  }
  // Stopped, it can be picked again.
  assert.ok(key(normaliseCard(rawCard()), "build", "agent").options);
});

test("Change agent on an approved card: the board asks first, then runs tuckerbox set", () => {
  const c = normaliseCard(rawCard({ column: "approved", state: "", step: "", round: 0 }), { on: null, path: "/work/shop" });
  assert.equal(voidsApproval(c, detail, "agent"), true);
  const r = choose(c, detail, "build", "agent", "codex");
  assert.match(r.confirm, /voids this change's approval/);
  assert.match(r.confirm, /Proposed/);
  // On confirmation the dialog runs the same set command.
  assert.deepEqual(invocation("set", c, r.extra).argv, ["tuckerbox", "set", "add-login", "step.build.agent=codex"]);
  // permissions asks too, and so does putting either back; model and effort never.
  assert.notEqual(choose(c, detail, "build", "permissions", "readonly").confirm, "");
  const own = normaliseCard(rawCard({ column: "approved", state: "", settings: stepSettings({ agent: S("codex", "change") }) }));
  assert.match(choose(own, detail, "review", "agent", null).confirm, /back to the repository's agent/);
  assert.equal(choose(c, detail, "build", "effort", "high").confirm, "");
  assert.equal(choose(c, detail, "build", "model", "sonnet").confirm, "");
  // Not approved: nothing to void.
  const stale = normaliseCard(rawCard({ approval: { state: "stale" } }));
  assert.equal(choose(stale, detail, "build", "agent", "codex").confirm, "");
  // The next read shows the card in Proposed with approval stale.
  const after = normaliseBoard({ ...fixture("detail.json"), cards: [{ ...rawCard(), column: "proposed", state: "", approval: { state: "stale", by: "Sam" },
    settings: [{ ...stepSettings()[0], agent: S("codex", "change") }, stepSettings()[1]] }] }).cards[0];
  assert.equal(after.column, "proposed");
  assert.equal(after.approval.state, "stale");
  assert.equal(key(after, "build", "agent").source, "change");
});

test("set never gets input it must not run", () => {
  const c = "add-login";
  assert.deepEqual(commandLine("set", c, { step: "build", key: "effort", value: "high" }), ["tuckerbox", "set", c, "step.build.effort=high"]);
  assert.throws(() => commandLine("set", c, { step: "build", key: "args", value: "x" }));
  assert.throws(() => commandLine("set", c, { step: "build", key: "command", value: "x" }));
  assert.throws(() => commandLine("set", c, { step: "../x", key: "model", value: "x" }));
  assert.throws(() => commandLine("set", c, { step: "", key: "model", value: "x" }));
  assert.throws(() => commandLine("set", c, { step: "build", key: "model", value: "--json" }));
  assert.throws(() => commandLine("set", c, { step: "build", key: "model", value: "a b" }));
  assert.throws(() => commandLine("set", c, { step: "build", key: "model", value: "" }));
  assert.throws(() => commandLine("set", c, { step: "build", key: "model", value: "a\nb" }));
});
