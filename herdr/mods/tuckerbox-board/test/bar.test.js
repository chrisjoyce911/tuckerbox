import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { text, find, callsOf } = await import("./stub/element.mjs");
const { fakeCx, gate } = await import("./stub/cx.mjs");
const { BarView, BarMenuView } = await import("../bar.js");
const { DrawerView } = await import("../drawer.js");
const { BoardView } = await import("../board.js");
const { windowSpec } = await import("../actions.js");

const at = { root: "/work/shop", cwd: "/work/shop", found: true, on: null, space: "s1" };
let serial = 0;
beforeEach(() => { reset(); serial++; });
const click = (el, id, cx) => callsOf(find(el, id), "on_click")[0][0]({}, cx);
const bar = (where = at, counts = null) => {
  const cx = fakeCx(), view = new BarView(); view.init({ at: where, counts }, cx); return { view, cx };
};
const panelLayout = (hidden = true, active = "files") => ({ id: "root", children: [{ id: "panel", hidden, children: [{ id: "panel-body", active, children: [{ id: "files" }, { id: "tuckerbox-panel" }] }] }] });
function space(path, machine = null) {
  state.spaces = [{ id: "s1", machine, active: true, tabs: [{ id: "t1", active: true }] }];
  state.panes = [{ pane: "p1", tab: "t1", cwd: path, terminal: "old" }]; state.focused = { pane: "p1" };
}
function repo(path, configured) {
  state.exec = async (cmd) => cmd === "git" ? { code: 0, stdout: `worktree ${path}\nHEAD 1\n` } : { code: 0, stdout: JSON.stringify({ version: 1, repo: path, path, cards: [], columns: [] }) };
  state.readTextFile = async (file) => file.endsWith("/.tuckerbox/config.toml") && configured() ? "" : null;
}

test("Space without a repository: empty render and resize to zero once", () => {
  const { view } = bar({ found: false, root: "" });
  assert.deepEqual(text(view.render()), []); view.render();
  assert.deepEqual(state.runs, [{ command: "layout.resize", args: { id: "tuckerbox-bar", px: 0 } }]);
});

test("Zoomed app: 150 pixels at 1.25 zoom sizes to 120 only on change", () => {
  state.props = { block: "my-bar" }; state.settings.zoom = 1.25;
  const { view } = bar(); const size = callsOf(view.render(), "on_size_change")[0][0];
  size({ width: 150 }); size({ width: 150 }); size({ width: 151 });
  assert.deepEqual(state.runs.map((x) => x.args), [{ id: "my-bar", px: 120 }, { id: "my-bar", px: 121 }]);
});

test("Badges: running and needs-you counts", () => {
  const { view } = bar(at, { running: 2, needs: 1 });
  assert.deepEqual(text(view.render()), ["Tuckerbox", "2", "1", "▾"]);
});

test("Button toggles the board twice through the existing mod.open command", () => {
  const { view, cx } = bar(); const rendered = view.render();
  click(rendered, "tuckerbox-toggle", cx); click(rendered, "tuckerbox-toggle", cx);
  assert.deepEqual(state.runs, Array(2).fill({ command: "mod.open", args: { name: "tuckerbox-board" } }));
});

test("Only set up without a config: drawer empty and toggle refused", async () => {
  const path = `/only-setup-${serial}`; space(path); repo(path, () => false);
  const { view } = bar({ ...at, found: false, root: path });
  assert.deepEqual(text(view.render()), ["Set up Tuckerbox"]);
  const cx = fakeCx(), drawer = new DrawerView(); drawer.init({ at: { found: false } }, cx);
  assert.deepEqual(text(drawer.render()), []);
  state.props = { overlay: "board" }; const board = new BoardView(); board.init({}, cx); board.refresh(cx); await cx.settle();
  assert.deepEqual(state.closed, ["board"]); assert.equal(state.notices[0].title, "No Tuckerbox config in this space");
});

test("Popup anchor is under the trigger at zoom; second click closes", () => {
  state.settings.zoom = 1.25; state.layout = panelLayout();
  const { view } = bar(); view.menu({ x: 500, y: 10, width: 30, height: 40 });
  assert.deepEqual(state.overlays[0].at, { x: 255, y: 56.25 });
  assert.equal(state.overlays[0].props.view, "bar-menu");
  view.menu({ x: 500, y: 10, width: 30, height: 40 });
  assert.deepEqual(state.closed, ["overlay-1"]); assert.equal(state.overlays.length, 1);
});

test("Dismissal on mouse down does not reopen the popup on the following click", () => {
  const { view } = bar(); view.menu({ x: 500, y: 0, width: 30, height: 40 });
  state.props = { ...state.overlays[0].props, overlay: "overlay-1" };
  const menu = new BarMenuView(); menu.init(); menu.close(); view.menu({ x: 500, y: 0, width: 30, height: 40 });
  assert.equal(state.overlays.length, 1);
});

test("Open Settings from the bar: board window gets start settings, menu closes", () => {
  state.props = { view: "bar-menu", overlay: "menu", token: "x" };
  const cx = fakeCx(), menu = new BarMenuView(); menu.init(); click(menu.render(), "bar-settings", cx);
  assert.deepEqual(state.overlays, [{ ...windowSpec("tuckerbox-board"), props: { start: "settings" } }]);
  assert.deepEqual(state.closed, ["menu"]);
});

test("Board beside the terminals: select/show, then hide when already showing", () => {
  state.layout = panelLayout(); state.props = { overlay: "menu" };
  const cx = fakeCx(), menu = new BarMenuView(); menu.init(); click(menu.render(), "bar-panel", cx);
  assert.deepEqual(state.runs, [{ command: "layout.select", args: { stack: "panel-body", child: "tuckerbox-panel" } }, { command: "layout.show", args: { id: "panel" } }]);
  state.layout = panelLayout(false, "tuckerbox-panel"); click(menu.render(), "bar-panel", cx);
  assert.deepEqual(state.runs.at(-1), { command: "layout.hide", args: { id: "panel" } });
  state.layout = null; assert.equal(find(menu.render(), "bar-panel"), null);
});

test("Panel board follows the shown space and omits pane/window controls", async () => {
  const path = `/panel-${serial}`; space(path); repo(path, () => true); state.props = { view: "panel", pane: "board-pane" };
  const cx = fakeCx(), board = new BoardView(); board.init({}, cx); board.refresh(cx); await cx.settle();
  assert.equal(board.at.root, path);
  const rendered = board.render(cx);
  for (const id of ["dock", "undock", "close-pane", "close"]) assert.equal(find(rendered, id), null);
  assert.equal(state.props.folder, undefined);
});

test("New project: init pane on the space machine, fresh config probe then board", async () => {
  const path = `/new-${serial}`; let config = false; space(path, "remote"); repo(path, () => config);
  const { view, cx } = bar({ ...at, found: false, root: path, cwd: path, on: "remote" });
  click(view.render(), "tuckerbox-setup", cx);
  assert.deepEqual(state.runs[0], { command: "terminal.open", args: { argv: ["tuckerbox", "init"], folder: path, place: "right", name: view.setup.name } });
  view.refresh(cx); await cx.settle(); assert.equal(view.at.found, false);
  config = true; state.terminals = [{ name: view.setup.name, state: "finished", exit_code: 0 }];
  view.refresh(cx); await cx.settle();
  assert.equal(view.at.found, true); assert.deepEqual(state.overlays, [windowSpec("tuckerbox-board")]);
  assert.deepEqual(text(view.render()), ["Tuckerbox", "▾"]);
});

test("Init fails: pane remains, no board, set up stays available", async () => {
  const path = `/failed-${serial}`; space(path); repo(path, () => false);
  const { view, cx } = bar({ ...at, found: false, root: path, cwd: path }); view.startSetup(cx);
  state.terminals = [{ name: view.setup.name, state: "finished", exit_code: 1 }];
  view.refresh(cx); await cx.settle();
  assert.deepEqual(state.overlays, []); assert.deepEqual(text(view.render()), ["Set up Tuckerbox"]);
  assert.equal(view.setup, null); assert.equal(state.runs.some((c) => c.command === "pane.close"), false);
});

test("Set up writes through init only; repeat click while pending starts one pane", () => {
  const { view, cx } = bar({ ...at, found: false });
  view.startSetup(cx); view.startSetup(cx);
  assert.equal(state.runs.length, 1); assert.deepEqual(state.runs[0].args.argv, ["tuckerbox", "init"]);
});

test("A late read for a previous space cannot change bar actions", async () => {
  const path = `/stale-${serial}`; space(path); const held = gate();
  state.exec = async () => { await held.p; return { code: 0, stdout: `worktree ${path}\nHEAD 1\n` }; };
  const { view, cx } = bar(); view.refresh(cx); await cx.settle();
  space(`/not-a-repo-${serial}`); view.follow("new-space", cx); held.open(); await cx.settle();
  assert.equal(view.at, null); assert.deepEqual(text(view.render()), []);
});

test("Init exit zero can race the first config probe: next read opens board", async () => {
  const path = `/exit-race-${serial}`; let config = false; space(path); repo(path, () => config);
  const { view, cx } = bar({ ...at, found: false, root: path, cwd: path }); view.startSetup(cx);
  state.terminals = [{ name: view.setup.name, state: "finished", exit_code: 0 }];
  view.refresh(cx); await cx.settle(); assert.deepEqual(state.overlays, []); assert.ok(view.setup);
  config = true; view.refresh(cx); await cx.settle(); assert.equal(state.overlays.length, 1);
});

test("Failed init never automatically opens a board even if a config appeared", async () => {
  const path = `/failed-config-${serial}`; space(path); repo(path, () => true);
  const { view, cx } = bar({ ...at, found: false, root: path, cwd: path }); view.startSetup(cx);
  state.terminals = [{ name: view.setup.name, state: "finished", exit_code: 1 }];
  view.refresh(cx); await cx.settle(); assert.deepEqual(state.overlays, []); assert.equal(view.setup, null);
});

test("Closing the init pane releases the setup button", async () => {
  const path = `/closed-init-${serial}`; space(path); repo(path, () => false);
  const { view, cx } = bar({ ...at, found: false, root: path, cwd: path }); view.startSetup(cx);
  state.terminals = [{ name: view.setup.name, state: "live" }]; view.refresh(cx); await cx.settle();
  state.terminals = []; view.refresh(cx); await cx.settle(); assert.equal(view.setup, null);
});
