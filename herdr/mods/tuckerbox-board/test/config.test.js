// openspec/changes/add-config-ui/specs/settings-view: the Settings view in
// the board. The real BoardView, SettingsView and data.js against the strict
// Herdr stub, with `tuckerbox config` stubbed: show --json and set --json
// as README.md ("Settings: the JSON it reads") gives them.   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { text, find, all, callsOf, idOf } = await import("./stub/element.mjs");
const { fakeCx, gate } = await import("./stub/cx.mjs");
const { BoardView } = await import("../board.js");
const cfgmod = await import("../config.js");
const { normaliseConfig, groups, sectionValue, workflowSteps, edit, addStep, removeStep, moveStep, setArgv, parseSet, canSave, changedUnder, tokenArgv, tokens, fileError } = cfgmod;

const fixture = JSON.parse(readFileSync(new URL("../fixtures/config.json", import.meta.url), "utf8"));
const clone = (x) => JSON.parse(JSON.stringify(x));

// ---- a stubbed machine ----

let docs, sets, setReply, held, shows, n = 0;

beforeEach(() => {
  reset();
  docs = {};       // cwd -> config show document
  sets = [];       // every config set: { args, cwd, on }
  shows = [];      // every config show: { cwd, on }
  setReply = () => ({ code: 0, stdout: JSON.stringify({ ok: true, dry_run: true, file: "repo", target: "local", reason: "no token for github.com on this machine", problems: [], changes: [] }) });
  held = null;
  const configs = new Set();
  state.readTextFile = async (path, opts) => (configs.has(`${opts?.on ?? ""}:${path}`) ? "[run]\n" : null);
  state.exec = async (cmd, args, opts) => {
    const on = opts?.on ?? null;
    if (cmd === "git" && args[0] === "worktree") {
      const root = Object.keys(docs).find((r) => opts.cwd === r || opts.cwd.startsWith(`${r}/`));
      if (!root) return { code: 128, stdout: "", stderr: "not a git repository" };
      configs.add(`${on ?? ""}:${root}/.tuckerbox/config.toml`);
      return { code: 0, stdout: `worktree ${root}\nHEAD 1\nbranch refs/heads/main\n`, stderr: "" };
    }
    if (cmd !== "tuckerbox") return { code: 1, stdout: "", stderr: "unexpected" };
    if (args[0] === "board") return { code: 0, stdout: JSON.stringify(boardDoc(opts.cwd)), stderr: "" };
    if (args[0] === "config" && args[1] === "show") {
      shows.push({ cwd: opts.cwd, on });
      if (held) { const g = gate(); held.set(opts.cwd, [...(held.get(opts.cwd) ?? []), g]); await g.p; }
      return { code: 0, stdout: JSON.stringify(docs[opts.cwd]), stderr: "" };
    }
    if (args[0] === "config" && args[1] === "set") {
      sets.push({ args, cwd: opts.cwd, on });
      return { stderr: "", ...setReply(args) };
    }
    return { code: 1, stdout: "", stderr: "unexpected" };
  };
});

const boardDoc = (path) => ({ version: 1, repo: path, path, columns: [{ id: "proposed", kind: "proposed", name: "Proposed" }], cards: [] });

// A repository (unique per test: data.js caches what a folder is) shown in one space.
function shop(doc = fixture, { machine = null } = {}) {
  const root = `/work/shop${++n}`;
  docs[root] = clone(doc);
  state.spaces = [{ id: "s1", machine, active: true, folder: null, tabs: [{ id: "t1", active: true }] }];
  state.panes = [{ pane: "p1", tab: "t1", cwd: root, terminal: true }];
  state.focused = { pane: "p1", space: "s1" };
  state.props = { overlay: "o1" };
  return root;
}

async function openBoard() {
  const cx = fakeCx();
  const v = new BoardView({});
  v.init({}, cx);
  cx.afters.shift()(cx);
  await cx.settle();
  return { v, cx };
}

const click = async (root, id, cx) => {
  const el = find(root, id);
  assert.ok(el, `no element ${id}; ids: ${all(root).map(idOf).filter(Boolean).join(" ")}`);
  const h = callsOf(el, "on_click")[0];
  assert.ok(h, `${id} is not clickable (disabled?)`);
  h[0]({}, cx);
  await cx.settle();
};

// Fires the dry-run timers the view set, then waits for the runs.
async function checks(cx) {
  while (cx.afters.length) cx.afters.shift()(cx);
  await cx.settle();
}

async function openSettings() {
  const { v, cx } = await openBoard();
  await click(v.render(), "settings", cx);
  return { v, cx, s: v.settings };
}

const sectionText = (v, s) => text(find(v.render(), `section-${s}`)).join(" ");

// ---- Opening Settings ----

test("Open and go back", async () => {
  const root = shop();
  const { v, cx, s } = await openSettings();
  let ui = v.render();
  assert.ok(find(ui, "settings"), "Settings in the window's header");
  assert.ok(find(ui, "section-repo"));
  assert.equal(find(ui, "columns-area"), null, "the view replaces the columns");
  assert.ok(text(ui).includes("This machine only (studio)"));
  assert.deepEqual(shows, [{ cwd: root, on: null }]);
  assert.equal(s.at.root, root);
  await click(ui, "settings-back", cx);
  ui = v.render();
  assert.equal(v.settings, null);
  assert.ok(find(ui, "columns-area"));
  assert.equal(find(ui, "section-repo"), null);
});

test("Opened as Settings: props.start", async () => {
  const root = shop();
  state.props = { overlay: "o1", start: "settings" };
  const { v } = await openBoard();
  assert.ok(v.settings, "Settings open at once");
  const ui = v.render();
  assert.ok(find(ui, "section-repo"));
  assert.equal(find(ui, "columns-area"), null);
  assert.deepEqual(shows, [{ cwd: root, on: null }]);
});

test("Settings in a docked pane too", async () => {
  const root = shop();
  state.props = { folder: root, machine: null, pane: "p9" };
  const { v } = await openBoard();
  assert.ok(find(v.render(), "settings"));
});

test("Space changed during a read", async () => {
  const a = shop();
  const docB = clone(fixture);
  docB.machine = "other-b";
  const { v, cx } = await openBoard();
  held = new Map();
  await click(v.render(), "settings", cx);
  // Switch the space to B while A's settings read is held.
  const b = `/work/shop${++n}`;
  docs[b] = docB;
  state.panes = [{ pane: "p1", tab: "t1", cwd: b, terminal: true }];
  cx.tick();
  await cx.settle();
  // A's read finishes last: it is never shown.
  held.get(b).shift().open();
  await cx.settle();
  held.get(a).shift().open();
  await cx.settle();
  const ui = text(v.render()).join(" ");
  assert.ok(ui.includes("This machine only (other-b)"), ui);
  assert.equal(v.settings.at.root, b);
  assert.ok(!ui.includes("(studio)"));
});

// ---- sections, values, groups ----

test("Machine-only key", async () => {
  shop();
  const { v } = await openSettings();
  const repo = sectionText(v, "repo"), machine = sectionText(v, "machine");
  assert.ok(machine.includes("This machine only (studio)"));
  assert.ok(machine.includes("PERMISSION PROFILES"));
  assert.ok(machine.includes("permissions.edit.allowed_remotes"));
  assert.ok(!repo.includes("permissions.edit"), "the Repository section offers no way to set profiles");
  assert.equal(find(v.render(), "field-repo-permissions.edit.allowed_remotes"), null);
  assert.ok(find(v.render(), "field-machine-permissions.edit.allowed_remotes-change"));
  assert.ok(machine.includes("Neither committed nor shared"));
  // repo-only keys are not in the machine section.
  assert.equal(find(v.render(), "field-machine-require_approval"), null);
});

test("Overridden machine default", async () => {
  shop();
  const { v } = await openSettings();
  const r = text(find(v.render(), "field-repo-max_parallel"));
  assert.ok(r.includes("2") && r.includes("repo"), r.join(" "));
  assert.ok(r.includes("machine config sets 4"), r.join(" "));
  const m = text(find(v.render(), "field-machine-max_parallel"));
  assert.ok(m.includes("4") && m.includes("machine"), m.join(" "));
  // The plain logic, too.
  const k = normaliseConfig(fixture).keys.find((x) => x.key === "max_parallel");
  assert.deepEqual(sectionValue(k, "machine"), { value: 4, source: "machine", note: "the repository sets 2" });
});

test("Unset key shows its default", () => {
  const k = normaliseConfig(fixture).keys.find((x) => x.key === "autopilot.interval");
  assert.equal(sectionValue(k, "repo").value, "5m");
  assert.equal(sectionValue(k, "machine").value, "5m");
});

test("Workflow group", async () => {
  shop();
  const cfg = normaliseConfig(fixture);
  assert.deepEqual(groups(cfg, "repo").map((g) => g.name), ["Workflow", "Runs", "Board", "Autopilot", "Change overrides"]);
  assert.deepEqual(groups(cfg, "machine").map((g) => g.name), ["Permission profiles", "Forge tokens", "Editor", "Other"]);
  // Either-scope keys (max_parallel, board.columns) are in the machine section's Other.
  assert.ok(groups(cfg, "machine").at(-1).keys.some((k) => k.key === "max_parallel"));
  assert.deepEqual(workflowSteps(cfg).map((s) => s.id), ["plan", "build", "review"]);
  assert.deepEqual(workflowSteps(cfg)[2].keys.map((k) => k.key), ["steps.review.agent", "steps.review.on_reject", "steps.review.max_rounds"]);
  const { v } = await openSettings();
  const wf = text(find(v.render(), "group-repo-workflow")).join(" ");
  assert.ok(wf.indexOf("plan") < wf.indexOf("build") && wf.indexOf("build") < wf.indexOf("review"), wf);
  const review = text(find(v.render(), "step-review"));
  assert.ok(review.includes("on_reject") && review.includes("build") && review.includes("repo"));
});

test("An unknown key goes to Other; empty groups hidden", () => {
  const d = { keys: [{ key: "frobnicate", value: 1, source: "repo", scope: "repo", file: "repo", type: "int" }] };
  assert.deepEqual(groups(normaliseConfig(d), "repo").map((g) => g.name), ["Other"]);
  assert.deepEqual(groups(normaliseConfig(d), "machine"), []);
});

// ---- editing the workflow ----

test("Reorder steps", async () => {
  const root = shop();
  const { v, cx } = await openSettings();
  await click(v.render(), "step-up-review", cx);
  assert.equal(v.settings.pending.repo.get("workflow.steps"), "plan,review,build");
  await checks(cx);
  await click(v.render(), "save-repo", cx);
  const save = sets.at(-1);
  assert.deepEqual(save.args, ["config", "set", "--json", "--if-match", "r1", "workflow.steps=plan,review,build"]);
  assert.equal(save.cwd, root);
});

test("Add and remove steps are pending edits", () => {
  const cfg = normaliseConfig(fixture);
  const p = addStep(new Map(), cfg, "docs", "claude");
  assert.deepEqual([...p], [["workflow.steps", "plan,build,review,docs"], ["steps.docs.agent", "claude"]]);
  assert.throws(() => addStep(new Map(), cfg, "build", "claude"), /already/);
  assert.throws(() => addStep(new Map(), cfg, "bad id", "claude"), /step id/);
  const r = removeStep(new Map(), cfg, "build");
  assert.equal(r.get("workflow.steps"), "plan,review");
  assert.equal(r.get("steps.build.agent"), null);
  assert.equal(r.get("steps.build.effort"), null);
  assert.deepEqual(setArgv("repo", r, "r1"), ["tuckerbox", "config", "set", "--json", "--if-match", "r1", "workflow.steps=plan,review", "--unset", "steps.build.agent", "--unset", "steps.build.effort"]);
  // Moving back where it was leaves nothing pending.
  const m = moveStep(moveStep(new Map(), cfg, "review", -1), cfg, "review", 1);
  assert.equal(m.size, 0);
});

// ---- checking and saving ----

test("Edit that adds a problem", async () => {
  shop();
  setReply = () => ({ code: 1, stdout: JSON.stringify({ ok: false, dry_run: true, file: "repo", target: "local", reason: "", problems: [{ file: "repo", key: "steps.review.on_reject", message: "no step bulid", new: true }], changes: [], error: { code: "check_failed", message: "the edit adds a problem" } }) });
  const { v, cx } = await openSettings();
  await click(v.render(), "field-repo-steps.review.on_reject-change", cx);
  v.settings.editing.box.set_value("bulid");
  await click(v.render(), "field-repo-steps.review.on_reject-apply", cx);
  await checks(cx);
  assert.ok(sets[0].args.includes("--dry-run"));
  assert.ok(sets[0].args.includes("steps.review.on_reject=bulid"));
  const ui = v.render();
  assert.ok(text(find(ui, "field-repo-steps.review.on_reject")).includes("no step bulid"));
  assert.equal(callsOf(find(ui, "save-repo"), "on_click").length, 0, "Save is disabled");
  assert.ok(callsOf(find(ui, "save-repo"), "opacity").length > 0);
});

test("Save as a pull request", async () => {
  shop();
  setReply = (args) => args.includes("--dry-run")
    ? { code: 0, stdout: JSON.stringify({ ok: true, dry_run: true, file: "repo", target: "pr", reason: "token for github.com", problems: [], changes: [] }) }
    : { code: 0, stdout: JSON.stringify({ ok: true, dry_run: false, file: "repo", target: "pr", reason: "", problems: [], changes: [], link: "https://github.com/example/shop/pull/9", message: "opened a pull request" }) };
  const { v, cx } = await openSettings();
  await click(v.render(), "field-repo-max_parallel-change", cx);
  v.settings.editing.box.set_value("3");
  await click(v.render(), "field-repo-max_parallel-apply", cx);
  await click(v.render(), "field-repo-ship-change", cx);
  await click(v.render(), "field-repo-ship-pick-merge", cx);
  await checks(cx);
  assert.ok(text(find(v.render(), "save-bar-repo")).join(" ").includes("Saves as a pull request: token for github.com"));
  const before = shows.length;
  await click(v.render(), "save-repo", cx);
  const saves = sets.filter((x) => !x.args.includes("--dry-run"));
  assert.equal(saves.length, 1, "one config set for both edits");
  assert.deepEqual(saves[0].args, ["config", "set", "--json", "--if-match", "r1", "max_parallel=3", "ship=merge"]);
  assert.ok(text(v.render()).join(" ").includes("https://github.com/example/shop/pull/9"));
  assert.ok(find(v.render(), "settings-link"));
  assert.equal(shows.length, before + 1, "reloaded");
  assert.equal(v.settings.pending.repo.size, 0);
});

test("Edits made while a save runs are kept", async () => {
  shop();
  setReply = (args) => args.includes("--dry-run")
    ? { code: 0, stdout: JSON.stringify({ ok: true, dry_run: true, file: "repo", target: "local", reason: "", problems: [], changes: [] }) }
    : { code: 0, stdout: JSON.stringify({ ok: true, dry_run: false, file: "repo", target: "local", reason: "", problems: [], changes: [], message: "saved" }) };
  const { v, cx } = await openSettings();
  await click(v.render(), "field-repo-max_parallel-change", cx);
  v.settings.editing.box.set_value("3");
  await click(v.render(), "field-repo-max_parallel-apply", cx);
  await checks(cx);
  // Hold the save's command.
  const exec = state.exec;
  const g = gate();
  state.exec = async (cmd, args, opts) => { if (args[1] === "set" && !args.includes("--dry-run")) await g.p; return exec(cmd, args, opts); };
  await click(v.render(), "save-repo", cx);
  // While it runs: the same field again, and another field.
  await click(v.render(), "field-repo-max_parallel-change", cx);
  v.settings.editing.box.set_value("5");
  await click(v.render(), "field-repo-max_parallel-apply", cx);
  await click(v.render(), "field-repo-ship-change", cx);
  await click(v.render(), "field-repo-ship-pick-merge", cx);
  g.open();
  await cx.settle();
  const save = sets.filter((x) => !x.args.includes("--dry-run"));
  assert.deepEqual(save.map((x) => x.args.slice(5)), [["max_parallel=3"]]);
  assert.deepEqual([...v.settings.pending.repo], [["max_parallel", "5"], ["ship", "merge"]], "the later edits stay");
  assert.ok(text(v.render()).join(" ").includes("2 edits made during the save are still pending"));
  // They are checked again.
  await checks(cx);
  assert.ok(sets.at(-1).args.includes("--dry-run") && sets.at(-1).args.includes("max_parallel=5"));
  assert.ok(callsOf(find(v.render(), "save-repo"), "on_click").length > 0, "and can be saved");
});

test("Machine edits save with --machine and the machine revision", () => {
  const cfg = normaliseConfig(fixture);
  const p = edit(new Map(), cfg, "machine", "editor", "hx");
  assert.deepEqual(setArgv("machine", p, cfg.files.machine.revision, { dryRun: true }), ["tuckerbox", "config", "set", "--machine", "--json", "--dry-run", "--if-match", "m1", "editor=hx"]);
  // An absent file: --if-match none.
  assert.ok(setArgv("machine", p, null).includes("none"));
  assert.throws(() => edit(new Map(), cfg, "repo", "permissions.edit.allowed_remotes", "x"), /this machine's config/);
  assert.throws(() => edit(new Map(), cfg, "machine", "require_approval", "false"), /repository config/);
  assert.throws(() => edit(new Map(), cfg, "repo", "max_parallel", "3\n4"), /line break/);
});

test("Command refused", async () => {
  shop();
  setReply = (args) => args.includes("--dry-run")
    ? { code: 0, stdout: JSON.stringify({ ok: true, dry_run: true, file: "repo", target: "local", reason: "no token", problems: [], changes: [] }) }
    : { code: 1, stdout: JSON.stringify({ ok: false, error: { code: "failed", message: "open /work/shop/.tuckerbox/config.toml: permission denied" }, problems: [], changes: [] }) };
  const { v, cx } = await openSettings();
  await click(v.render(), "field-repo-max_parallel-change", cx);
  v.settings.editing.box.set_value("3");
  await click(v.render(), "field-repo-max_parallel-apply", cx);
  await checks(cx);
  await click(v.render(), "save-repo", cx);
  assert.ok(text(v.render()).join(" ").includes("permission denied"));
  assert.equal(v.settings.pending.repo.get("max_parallel"), "3", "the edits stay");
  // Output that is not the JSON document: its text is shown.
  assert.equal(parseSet({ code: 2, stdout: "", stderr: "unknown command config" }).error.message, "unknown command config");
});

test("Someone saved first", async () => {
  const root = shop();
  setReply = (args) => args.includes("--dry-run")
    ? { code: 0, stdout: JSON.stringify({ ok: true, dry_run: true, file: "repo", target: "local", reason: "", problems: [], changes: [] }) }
    : { code: 1, stdout: JSON.stringify({ ok: false, error: { code: "changed_since_read", message: "changed since read" }, problems: [], changes: [] }) };
  const { v, cx } = await openSettings();
  await click(v.render(), "field-repo-max_parallel-change", cx);
  v.settings.editing.box.set_value("5");
  await click(v.render(), "field-repo-max_parallel-apply", cx);
  await checks(cx);
  // A terminal saves max_parallel = 3 meanwhile.
  const moved = docs[root].keys.find((k) => k.key === "max_parallel");
  moved.value = 3;
  docs[root].files.repo.revision = "r2";
  await click(v.render(), "save-repo", cx);
  assert.equal(v.settings.pending.repo.get("max_parallel"), "5", "edits kept");
  assert.ok(v.settings.conflicts.repo.has("max_parallel"));
  assert.ok(text(find(v.render(), "field-repo-max_parallel")).join(" ").includes("changed by someone else"));
  // Checked again against the new revision; save again.
  await checks(cx);
  assert.ok(sets.at(-1).args.includes("r2"));
});

test("changedUnder marks only keys that moved", () => {
  const a = normaliseConfig(fixture);
  const b2 = clone(fixture);
  b2.keys.find((k) => k.key === "editor").value = "vim";
  const b = normaliseConfig(b2);
  const p = new Map([["editor", "hx"], ["max_parallel", "3"]]);
  assert.deepEqual([...changedUnder(a, b, "machine", p)], ["editor"]);
});

test("canSave needs a clean check of the pending edits", () => {
  const p = new Map([["max_parallel", "3"]]);
  assert.equal(canSave(p, null), false);
  assert.equal(canSave(p, { problems: [{ key: "x", new: false }], error: null }), true, "old problems don't block");
  assert.equal(canSave(p, { problems: [{ key: "x", new: true }], error: null }), false);
  assert.equal(canSave(new Map(), { problems: [], error: null }), false);
});

// ---- tokens and broken files ----

test("Set a token", async () => {
  const root = shop();
  const { v, cx } = await openSettings();
  assert.deepEqual(tokens(v.settings.config).map((t) => [t.host, t.set, t.source]), [["github.com", true, "credentials"], ["gitlab.com", false, ""]]);
  let ui = text(find(v.render(), "token-github.com")).join(" ");
  assert.ok(ui.includes("set") && ui.includes("from the credentials file"));
  await click(v.render(), "set-token-gitlab.com", cx);
  const call = state.runs.at(-1);
  assert.equal(call.command, "terminal.open");
  assert.deepEqual(call.args.argv, ["tuckerbox", "config", "set", "--machine", 'forges."gitlab.com".token']);
  assert.equal(call.args.folder, root);
  // The pane opens, then closes: the view reads again and shows it set.
  state.panes = [...state.panes, { pane: "p2", tab: "t1", cwd: root, terminal: true }];
  cx.tick(); await cx.settle();
  docs[root].keys.find((k) => k.key === 'forges."gitlab.com".token').set = true;
  docs[root].keys.find((k) => k.key === 'forges."gitlab.com".token').source = "credentials";
  state.panes = state.panes.filter((p) => p.pane !== "p2");
  cx.tick(); await cx.settle();
  ui = text(find(v.render(), "token-gitlab.com")).join(" ");
  assert.ok(ui.includes("from the credentials file"), ui);
  // No token value anywhere: not in commands, not in the view's state.
  assert.ok(!call.args.argv.some((a) => a.includes("=")));
  assert.equal(normaliseConfig({ keys: [{ key: 'forges."x".token', secret: true, value: "abc123" }] }).keys[0].value, null);
  assert.throws(() => tokenArgv("editor"), /forge token/);
  assert.throws(() => edit(new Map(), v.settings.config, "machine", 'forges."github.com".token', "abc"), /Set token/);
});

test("Set token on a board pinned to another machine opens there", async () => {
  const root = shop(fixture, { machine: "vps" });
  state.focused = { pane: "p1", space: "elsewhere" };
  state.spaces.push({ id: "elsewhere", machine: null, active: false, tabs: [] });
  const { v, cx } = await openSettings();
  assert.equal(shows[0].on, "vps");
  await click(v.render(), "set-token-github.com", cx);
  const call = state.runs.at(-1);
  assert.equal(call.command, "space.new");
  assert.equal(call.args.machine, "vps");
  assert.equal(call.args.folder, root);
});

test("Syntax error in repository config", async () => {
  const doc = clone(fixture);
  doc.files.repo.error = { message: "expected '=' after a key", line: 12 };
  doc.files.repo.revision = "r1";
  shop(doc);
  const { v, cx } = await openSettings();
  const cfg = v.settings.config;
  assert.equal(fileError(cfg, "repo"), `/work/shop${n}/.tuckerbox/config.toml:12: expected '=' after a key`.replace(`/work/shop${n}`, "/work/shop"));
  const repo = sectionText(v, "repo");
  assert.ok(repo.includes(":12: expected '=' after a key"), repo);
  assert.equal(find(v.render(), "field-repo-max_parallel"), null, "no fields for that file");
  assert.ok(find(v.render(), "field-machine-editor"), "the machine section still works");
  await click(v.render(), "edit-file-repo", cx);
  assert.deepEqual(state.runs.at(-1).args.argv, ["tuckerbox", "config", "edit"]);
});

test("Edit file for machine config adds --machine", async () => {
  const doc = clone(fixture);
  doc.files.machine.error = { message: "bad", line: 3 };
  shop(doc);
  const { v, cx } = await openSettings();
  await click(v.render(), "edit-file-machine", cx);
  assert.deepEqual(state.runs.at(-1).args.argv, ["tuckerbox", "config", "edit", "--machine"]);
});

test("A tuckerbox without config show: its message", async () => {
  const root = shop();
  docs[root] = {};
  const { v } = await openSettings();
  assert.ok(text(v.render()).join(" ").includes("gave no key list"));
});
