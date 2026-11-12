// Opening, hiding and docking the board (herdr-board: Opening the board,
// Docking the board): the Herdr commands each one runs.   node --test test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dockCall, undockCalls, toggleCall, windowSpec } from "../actions.js";

const MOD = "tuckerbox-board";
const json = (name) => JSON.parse(readFileSync(new URL(`../${name}`, import.meta.url), "utf8"));

const spaces = [
  { id: "s1", machine: null, tabs: [{ id: "t1", active: false }, { id: "t2", active: true }] },
  { id: "mini/s2", machine: "mini", tabs: [{ id: "mini/t3", active: true }] },
];
const panes = [
  { pane: "p1", tab: "t1", terminal: "a1", cwd: "/work/shop" },
  { pane: "p2", tab: "t2", terminal: "a2", cwd: "/work/shop/web" },
  { pane: "p3", tab: "t2", terminal: null, mod: MOD },
  { pane: "mini/p4", tab: "mini/t3", terminal: "mini/a4", cwd: "/srv/shop" },
];

test("Toggle key: mod-shipped keys.json binds mod.open of the board, which mod.json makes a window", () => {
  const keys = json("keys.json");
  assert.deepEqual(Object.keys(keys), ["global"]);
  assert.deepEqual(Object.values(keys.global), [`mod.open:${MOD}`]);
  assert.deepEqual(toggleCall(MOD), { command: "mod.open", args: { name: MOD } });
  const overlay = json("mod.json").overlay;
  assert.equal(overlay.kind, "window");
  // Undock opens the same window mod.open toggles.
  const spec = windowSpec(MOD);
  assert.deepEqual({ kind: spec.kind, width: spec.width, height: spec.height, key: spec.key, keep: spec.keep }, overlay);
  assert.equal(json("mod.json").place, "none", "never docked by itself on first sight");
});

test("Dock: a pane beside the focused terminal, pinned to the repository and machine", () => {
  const call = dockCall({ root: "/work/shop", on: null, space: "s1", spaces, panes, focused: { pane: "p1" }, mod: MOD });
  assert.deepEqual(call, { command: "pane.split", args: { direction: "right", pane: "p1", mod: MOD, props: { folder: "/work/shop", machine: null } } });
});

test("Dock: the shown tab's terminal when focus is on a board pane or elsewhere", () => {
  assert.equal(dockCall({ root: "/work/shop", space: "s1", spaces, panes, focused: { pane: "p3" }, mod: MOD }).args.pane, "p2");
  assert.equal(dockCall({ root: "/work/shop", space: "s1", spaces, panes, focused: { pane: "mini/p4" }, mod: MOD }).args.pane, "p2",
    "focus in another space is never split");
});

test("Dock on another machine: props name that machine", () => {
  const call = dockCall({ root: "/srv/shop", on: "mini", space: "mini/s2", spaces, panes, focused: { pane: "mini/p4" }, mod: MOD });
  assert.deepEqual(call.args, { direction: "right", pane: "mini/p4", mod: MOD, props: { folder: "/srv/shop", machine: "mini" } });
});

test("Dock with no terminal in the space: a tab with the board as its pane", () => {
  const call = dockCall({ root: "/work/shop", on: null, space: "s1", spaces, panes: [], focused: null, mod: MOD });
  assert.deepEqual(call, { command: "tab.new", args: { space: "s1", name: "Tuckerbox", mod: MOD, props: { folder: "/work/shop", machine: null } } });
});

test("No Dock without a repository", () => {
  assert.throws(() => dockCall({ root: "", space: "s1", spaces, panes, focused: null, mod: MOD }), /no Tuckerbox repository/);
});

test("Undock: the window again, then this pane closes; without its pane id, nothing", () => {
  assert.deepEqual(undockCalls("p3", MOD), { overlay: windowSpec(MOD), close: { command: "pane.close", args: { pane: "p3" } } });
  assert.throws(() => undockCalls(null, MOD), /doesn't know its pane/);
});
