// Autopilot from the top bar's menu (add-herdr-topbar: Autopilot from the
// bar; board-data: Autopilot in the board document). bar-autopilot.js
// against the strict Herdr stub, with a fake hc that records its calls.
//   node --test test/
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { all, idOf, find, callsOf, text } = await import("./stub/element.mjs");
const { fakeCx } = await import("./stub/cx.mjs");
const { normaliseBoard } = await import("../model.js");
const { autopilotName, autopilotMenu, startArgs, stopArgs, startAutopilot, stopAutopilot, sha256Hex, autopilotArgv, renderAutopilot, forgetAutopilot } = await import("../bar-autopilot.js");

const REPO = "github.com/acme/shop";
const NAME = autopilotName(REPO);
const doc = (autopilot, extra = {}) => normaliseBoard({ version: 1, repo: REPO, path: "/src/shop", os: "darwin", require_approval: true, autopilot, ...extra }, { on: extra.on ?? null });

let calls;
beforeEach(() => {
  reset();
  calls = [];
  state.hc = async (args) => { calls.push(args); return { code: 0, stdout: "{}", stderr: "" }; };
});

test("SHA-256 matches node's", () => {
  for (const s of ["", "abc", REPO, "/Users/someone/src/shop", "ünïcødé ✓ 🦘", "x".repeat(200)]) {
    assert.equal(sha256Hex(new TextEncoder().encode(s)), createHash("sha256").update(s).digest("hex"), s);
  }
});

test("the terminal is named tb-ap- and 12 hex digits of the repository's hash", () => {
  assert.match(NAME, /^tb-ap-[0-9a-f]{12}$/);
  assert.equal(NAME, "tb-ap-" + createHash("sha256").update(REPO).digest("hex").slice(0, 12));
  assert.notEqual(autopilotName("github.com/acme/other"), NAME);
  // herdr-core names: at most 32 of a-z 0-9 - _.
  assert.ok(NAME.length <= 32);
});

test("Start and stop: hc runs tuckerbox autopilot in the named terminal, then sends it Ctrl-C", async () => {
  const off = doc({ running: false });
  assert.deepEqual(autopilotMenu(off), { status: "Autopilot is off", detail: "", actions: ["start"] });

  const started = await startAutopilot(off);
  assert.equal(started.ok, true);
  assert.deepEqual(calls[0], [
    "run", "--json", "--bg", "--name", NAME, "--cwd", "/src/shop",
    "--label", "herdr.space=shop", "--label", "herdr.tab=autopilot", "--no-parent",
    "--", "env", `TUCKERBOX_TERMINAL=${NAME}`, "tuckerbox", "autopilot",
  ]);

  // The next read of the board finds autopilot in that terminal: the menu
  // says it runs and offers Stop.
  const on = doc({ running: true, pid: 4242, machine: "mini", started: "2026-11-13T09:00:00+10:00", terminal: NAME });
  const menu = autopilotMenu(on);
  assert.equal(menu.status, "Autopilot runs");
  assert.match(menu.detail, /pid 4242 on mini/);
  assert.deepEqual(menu.actions, ["stop"]);

  const stopped = await stopAutopilot(on);
  assert.equal(stopped.ok, true);
  assert.deepEqual(calls[1], ["keys", "--json", NAME, "ctrl+c"]);

  // After Ctrl-C autopilot exits and its lock goes: off again.
  assert.deepEqual(autopilotMenu(doc({ running: false })).actions, ["start"]);
  // The bar called nothing but hc: no exec, no Herdr actions.
  assert.deepEqual(state.runs, []);
});

test("Start and stop on another machine go there", async () => {
  const b = doc({ running: true, pid: 7, terminal: NAME }, { on: "mini", os: "linux" });
  assert.deepEqual(startArgs(b).slice(5, 7), ["--on", "mini"]);
  assert.deepEqual(stopArgs(b), ["keys", "--json", `mini/${NAME}`, "ctrl+c"]);
});

test("Start on Windows sets the terminal's name through PowerShell", () => {
  const argv = autopilotArgv(NAME, "windows");
  assert.equal(argv[0], "powershell");
  assert.match(argv.at(-1), new RegExp(`\\$env:TUCKERBOX_TERMINAL='${NAME}'; tuckerbox autopilot`));
  assert.deepEqual(startArgs(doc({ running: false }, { os: "windows" })).slice(-6), ["--", ...argv]);
});

test("Started in a shell: runs, with its pid, and no Stop", () => {
  const menu = autopilotMenu(doc({ running: true, pid: 5150, machine: "mini", started: "2026-11-13T09:00:00+10:00" }));
  assert.equal(menu.status, "Autopilot runs");
  assert.match(menu.detail, /pid 5150/);
  assert.deepEqual(menu.actions, []);
  // A terminal of another name (another repository's, or a person's) is not ours either.
  assert.deepEqual(autopilotMenu(doc({ running: true, pid: 1, terminal: "tb-ap-000000000000" })).actions, []);
});

test("Approval off: no Autopilot item", () => {
  assert.equal(autopilotMenu(doc({ running: false }, { require_approval: false })), null);
  assert.equal(autopilotMenu({ error: { code: "x", message: "y" } }), null);
  assert.equal(autopilotMenu(null), null);
});

test("Without hc, Start says how to run autopilot in a terminal", async () => {
  state.hc = async () => { throw new Error("spawn hc ENOENT"); };
  const r = await startAutopilot(doc({ running: false }));
  assert.equal(r.ok, false);
  assert.equal(r.hint, true);
  assert.match(r.message, /run `tuckerbox autopilot` in a terminal in \/src\/shop/);

  state.hc = async () => ({ code: 127, stdout: "", stderr: "sh: hc: command not found" });
  const remote = await startAutopilot(doc({ running: false }, { on: "mini" }));
  assert.equal(remote.hint, true);
  assert.match(remote.message, /on mini/);
});

test("hc's own errors are shown as they are", async () => {
  state.hc = async () => ({ code: 1, stdout: "", stderr: JSON.stringify({ error: { kind: "name_taken", message: `a live terminal is named ${NAME}` } }) });
  const r = await startAutopilot(doc({ running: false }));
  assert.equal(r.ok, false);
  assert.ok(!r.hint);
  assert.match(r.message, /a live terminal is named/);
  state.hc = async () => ({ code: 1, stdout: "", stderr: `no terminal ${NAME} found` });
  const s = await stopAutopilot(doc({ running: true, terminal: NAME }));
  assert.ok(!s.hint, "a missing terminal is not a missing hc");
});

// ---- The menu's section, as the bar menu's item() draws it ----

// The shown space is /src/shop, a Tuckerbox repository whose board
// document reports `autopilot`; item() records what the menu would draw.
function space(autopilot, extra = {}) {
  state.spaces = [{ id: "s1", machine: null, active: true, folder: null, tabs: [{ id: "t1", active: true }] }];
  state.panes = [{ pane: "p1", tab: "t1", cwd: "/src/shop" }];
  state.focused = { pane: "p1" };
  state.readTextFile = async (path) => (path === "/src/shop/.tuckerbox/config.toml" ? "[run]\n" : null);
  state.exec = async (cmd, args) => {
    if (cmd === "git" && args[0] === "worktree") return { code: 0, stdout: "worktree /src/shop\nHEAD 1\nbranch refs/heads/main\n", stderr: "" };
    if (cmd === "tuckerbox") return { code: 0, stdout: JSON.stringify({ version: 1, repo: REPO, path: "/src/shop", os: "darwin", require_approval: true, autopilot: autopilot(), ...extra }), stderr: "" };
    return { code: 1, stdout: "", stderr: "unexpected" };
  };
}
const items = [];
const item = (id, label, action) => { items.push({ id, label, action }); return { id, label }; };
async function draw(cx) {
  items.length = 0;
  forgetAutopilot();
  renderAutopilot(item, cx);
  await cx.settle();
  items.length = 0;
  const el = renderAutopilot(item, cx);
  return { el, ids: all(el).map(idOf).filter(Boolean), items: [...items] };
}
const statusOf = (el) => all(el).find((x) => idOf(x) === "bar-autopilot-status")?.kids.join("");

test("Start and stop from the menu: hc runs it, the menu says it runs, Stop sends Ctrl-C", async () => {
  let ap = { running: false };
  space(() => ap);
  const cx = fakeCx();
  let m = await draw(cx);
  assert.match(statusOf(m.el), /Autopilot is off/);
  assert.deepEqual(m.items.map((i) => i.label), ["Start autopilot"]);
  m.items[0].action();
  await cx.settle();
  assert.equal(calls[0][0], "run");
  assert.ok(calls[0].includes(NAME));
  assert.equal(state.notices.at(-1).level, "info");

  ap = { running: true, pid: 4242, machine: "mini", started: "2026-11-13T09:00:00+10:00", terminal: NAME };
  m = await draw(cx);
  assert.match(statusOf(m.el), /Autopilot runs · pid 4242 on mini/);
  assert.deepEqual(m.items.map((i) => i.label), ["Stop autopilot"]);
  m.items[0].action();
  await cx.settle();
  assert.deepEqual(calls[1], ["keys", "--json", NAME, "ctrl+c"]);

  ap = { running: false };
  m = await draw(cx);
  assert.deepEqual(m.items.map((i) => i.label), ["Start autopilot"]);
});

test("Started in a shell, from the menu: running with its pid, no Stop", async () => {
  space(() => ({ running: true, pid: 5150, machine: "mini", started: "2026-11-13T09:00:00+10:00" }));
  const m = await draw(fakeCx());
  assert.match(statusOf(m.el), /pid 5150/);
  assert.deepEqual(m.items, []);
});

test("Approval off, from the menu: no Autopilot section", async () => {
  space(() => ({ running: false }), { require_approval: false });
  const m = await draw(fakeCx());
  assert.deepEqual(m.ids, []);
  assert.deepEqual(m.items, []);
});

// ---- Through the production entry point: main.js routes view "bar-menu" ----

const { default: Main } = await import("../main.js");
const { BarView } = await import("../bar.js");
const click = (el, id, cx) => callsOf(find(el, id), "on_click")[0][0]({}, cx);

async function openMenu(cx) {
  state.props = { view: "bar-menu", overlay: "menu-1", token: "t" };
  forgetAutopilot();
  const main = new Main(); main.init({}, cx);
  main.render(cx); await cx.settle();
  return main;
}

test("The bar menu (main.js, view bar-menu) shows Autopilot; Start runs hc and closes the menu", async () => {
  let ap = { running: false };
  space(() => ap);
  const cx = fakeCx();
  let main = await openMenu(cx);
  let el = main.render(cx);
  assert.deepEqual(text(el), ["Board", "Settings", "Autopilot is off", "Start autopilot"]);
  click(el, "bar-autopilot-start", cx);
  await cx.settle();
  assert.equal(calls[0][0], "run");
  assert.ok(calls[0].includes(NAME));
  assert.deepEqual(state.closed, ["menu-1"]);

  ap = { running: true, pid: 4242, machine: "mini", started: "2026-11-13T09:00:00+10:00", terminal: NAME };
  main = await openMenu(cx);
  el = main.render(cx);
  assert.ok(text(el).includes("Stop autopilot"));
  click(el, "bar-autopilot-stop", cx);
  await cx.settle();
  assert.deepEqual(calls[1], ["keys", "--json", NAME, "ctrl+c"]);
});

test("The bar menu (main.js) has no Autopilot with approval off, or started in a shell no Stop", async () => {
  space(() => ({ running: false }), { require_approval: false });
  const cx = fakeCx();
  let el = (await openMenu(cx)).render(cx);
  assert.deepEqual(text(el), ["Board", "Settings"]);
  space(() => ({ running: true, pid: 5150, machine: "mini" }));
  el = (await openMenu(cx)).render(cx);
  assert.equal(find(el, "bar-autopilot-stop"), null);
  assert.equal(find(el, "bar-autopilot-start"), null);
  assert.match(text(el).join(" "), /pid 5150/);
});

test("The popup is tall enough for the Autopilot section when approval is required", async () => {
  for (const [approval, height] of [[true, 84 + 56], [false, 84]]) {
    reset(); state.hc = async () => ({ code: 0 });
    space(() => ({ running: false }), { require_approval: approval, columns: [], cards: [] });
    const cx = fakeCx(), bar = new BarView(); bar.init({}, cx);
    bar.refresh(cx); await cx.settle();
    bar.menu({ x: 500, y: 0, width: 30, height: 40 });
    assert.equal(state.overlays.at(-1).height, height, `approval ${approval}`);
  }
});
