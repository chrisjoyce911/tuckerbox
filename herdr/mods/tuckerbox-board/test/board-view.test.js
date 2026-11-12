// openspec/changes/fix-board-safety/specs/board-view: which repository and
// machine the board reads, and reads that finish after the space changed.
// The real data.js, DrawerView and BoardView against the strict Herdr stub,
// with a fake exec whose reads the test releases in a chosen order.
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
await import("./stub/register.mjs");
const { state, reset } = await import("./stub/herdr.mjs");
const { text } = await import("./stub/element.mjs");
const { fakeCx, gate } = await import("./stub/cx.mjs");
const { locate, loadBoard } = await import("../data.js");
const { repoCandidates, mainWorktreeOf } = await import("../model.js");
const { DrawerView } = await import("../drawer.js");
const { BoardView } = await import("../board.js");

// A machine's file system: folder -> the git worktree list a folder in it
// prints, and which checkouts have a Tuckerbox config.
let git, configs, reads, held;

beforeEach(() => {
  reset();
  git = {};
  configs = new Set();
  reads = [];      // every tuckerbox read: { cwd, on }
  held = null;     // when a Map, tuckerbox reads wait for release(cwd)
  state.readTextFile = async (path, opts) => (configs.has(`${opts?.on ?? ""}:${path}`) ? "[run]\n" : null);
  state.exec = async (cmd, args, opts) => {
    const on = opts?.on ?? "";
    if (cmd === "git" && args[0] === "worktree") {
      const repo = Object.keys(git).find((k) => `${on}:${opts.cwd}`.startsWith(k));
      return repo ? { code: 0, stdout: git[repo], stderr: "" } : { code: 128, stdout: "", stderr: "not a git repository" };
    }
    if (cmd === "git" && args[0] === "rev-parse") {
      const repo = Object.keys(git).find((k) => `${on}:${opts.cwd}`.startsWith(k));
      return repo ? { code: 0, stdout: repo.slice(repo.indexOf(":") + 1) + "\n" } : { code: 128, stdout: "" };
    }
    if (cmd === "tuckerbox") {
      reads.push({ cwd: opts.cwd, on: opts.on ?? null });
      if (held) { const g = gate(); held.set(opts.cwd, [...(held.get(opts.cwd) ?? []), g]); await g.p; }
      return { code: 0, stdout: JSON.stringify(boardDoc(opts.cwd)), stderr: "" };
    }
    return { code: 1, stdout: "", stderr: "unexpected" };
  };
});

function repo(path, { on = "", config = true, worktrees = [] } = {}) {
  const list = [`worktree ${path}\nHEAD 1111\nbranch refs/heads/main\n`, ...worktrees.map((w) => `worktree ${w}\nHEAD 2222\nbranch refs/heads/x\n`)];
  git[`${on}:${path}`] = list.join("\n");
  for (const w of worktrees) git[`${on}:${w}`] = list.join("\n");
  if (config) configs.add(`${on}:${path}/.tuckerbox/config.toml`);
}

function boardDoc(path) {
  const name = path.split("/").pop();
  return {
    version: 1, repo: path, path,
    columns: [{ id: "proposed", kind: "proposed", name: "Proposed" }, { id: "in_progress", kind: "step", name: "In progress" }],
    cards: [{ change: `${name}-change`, title: `${name} change`, column: "in_progress", state: name === "a" ? "running" : "blocked" }],
  };
}

const release = async (cwd, cx) => { const g = held.get(cwd).shift(); g.open(); await cx.settle(); };

function oneSpace(cwd, { id = "s1", machine = null } = {}) {
  state.spaces = [{ id, machine, active: true, folder: null, tabs: [{ id: "t1", active: true }] }];
  state.panes = [{ pane: "p1", tab: "t1", cwd }];
  state.focused = { pane: "p1" };
}

// ---- The repository's main checkout ----

test("mainWorktreeOf: the first entry, null when it is bare", () => {
  assert.equal(mainWorktreeOf("worktree /work/shop\nHEAD 1\nbranch refs/heads/main\n\nworktree /w/x\nHEAD 2\n"), "/work/shop");
  assert.equal(mainWorktreeOf("worktree /srv/shop.git\nbare\n\nworktree /srv/shop\nHEAD 1\n"), null);
  assert.equal(mainWorktreeOf(""), null);
});

test("Pane in a change's worktree: the board reads the main checkout", async () => {
  repo("/work/shop", { worktrees: ["/work/shop-worktrees/add-login"] });
  oneSpace("/work/shop-worktrees/add-login");
  const at = await locate(null);
  assert.equal(at.found, true);
  assert.equal(at.root, "/work/shop");
  await loadBoard(at);
  assert.deepEqual(reads, [{ cwd: "/work/shop", on: null }]);
});

test("Docked on a worktree folder: it reads the main checkout", async () => {
  repo("/work/shop2", { worktrees: ["/work/shop2-worktrees/add-login"] });
  oneSpace("/elsewhere");
  state.props = { folder: "/work/shop2-worktrees/add-login", machine: null };
  assert.equal((await locate(null)).root, "/work/shop2");
});

test("Plain checkout: the board reads it, as before", async () => {
  repo("/work/shop3");
  oneSpace("/work/shop3");
  assert.equal((await locate(null)).root, "/work/shop3");
});

test("A bare main worktree: the folder's own top level", async () => {
  git[":/srv/bare-shop"] = "worktree /srv/bare-shop.git\nbare\n\nworktree /srv/bare-shop\nHEAD 1\n";
  configs.add(":/srv/bare-shop/.tuckerbox/config.toml");
  oneSpace("/srv/bare-shop");
  assert.equal((await locate(null)).root, "/srv/bare-shop");
});

// ---- Explicit machine pin ----

test("Pinned to this computer in a remote space: reads here, not on vps", async () => {
  const spaces = [{ id: "s1", machine: "vps", active: true, folder: null, tabs: [] }];
  assert.equal(repoCandidates({ props: { folder: "/local/shop", machine: null }, spaces, panes: [], focused: null }).on, null);
  repo("/local/shop");
  repo("/local/shop", { on: "vps" });
  state.spaces = spaces;
  state.props = { folder: "/local/shop", machine: null };
  const at = await locate(null);
  assert.equal(at.on, null);
  await loadBoard(at);
  assert.deepEqual(reads, [{ cwd: "/local/shop", on: null }]);
});

test("No machine prop: the space's machine", async () => {
  const spaces = [{ id: "s1", machine: "vps", active: true, folder: null, tabs: [] }];
  assert.equal(repoCandidates({ props: { folder: "/srv/shop" }, spaces, panes: [], focused: null }).on, "vps");
  repo("/srv/shop", { on: "vps" });
  state.spaces = spaces;
  state.props = { folder: "/srv/shop" };
  const at = await locate(null);
  assert.equal(at.on, "vps");
  assert.equal(at.root, "/srv/shop");
});

// ---- Stale reads are dropped ----

// Spaces A, B and C, each with its folder; `show` makes one the shown space.
function spacesABC() {
  repo("/repo/a");
  repo("/repo/b", { config: false });
  repo("/repo/c");
  state.panes = [];
  state.focused = null;
  return (id) => {
    state.spaces = ["a", "b", "c"].map((x) => ({ id: x, machine: null, active: x === id, folder: `/repo/${x}`, tabs: [] }));
  };
}

function drawer(cx) {
  const v = new DrawerView();
  v.init({}, cx);
  return v;
}

test("Drawer moves to a space without a config: A's counts are never shown in B", async () => {
  const show = spacesABC();
  held = new Map();
  const cx = fakeCx();
  show("a");
  const v = drawer(cx);
  cx.afters[0](cx);
  await cx.settle();
  assert.equal(held.get("/repo/a").length, 1, "A's read is running");

  show("b");
  cx.tick();
  assert.equal(v.at, null, "A's result isn't kept for B");
  await cx.settle();
  assert.equal(v.at?.found, false, "B has no config");
  await release("/repo/a", cx);
  assert.equal(v.at?.found, false, "A's late read is dropped");
  assert.equal(v.counts, null);
  assert.deepEqual(text(v.render()), [], "the drawer draws nothing in B");
});

test("Back to the first space: A, then C, then A; C's read finishes after A's", async () => {
  const show = spacesABC();
  held = new Map();
  const cx = fakeCx();
  show("a");
  const v = drawer(cx);
  cx.afters[0](cx);
  await cx.settle();
  show("c");
  cx.tick();
  await cx.settle();
  assert.equal(held.get("/repo/c").length, 1, "C's read started while A's runs");
  show("a");
  cx.tick();
  await cx.settle();
  assert.equal(held.get("/repo/a").length, 1, "A's running read is reused, not doubled");

  await release("/repo/a", cx);
  assert.equal(v.repo, "a");
  assert.equal(v.counts.running, 1);
  await release("/repo/c", cx);
  assert.equal(v.repo, "a", "C's result is discarded");
  assert.equal(v.counts.running, 1);
  assert.ok(text(v.render()).includes("a"));
});

test("Board window moves to another repository: A's result is discarded, C's read starts at once", async () => {
  const show = spacesABC();
  held = new Map();
  const cx = fakeCx();
  show("a");
  state.props = { overlay: "o1" };
  const v = new BoardView();
  v.init({}, cx);
  cx.afters[0](cx);
  await cx.settle();
  assert.equal(held.get("/repo/a").length, 1);

  show("c");
  cx.tick();
  assert.equal(v.board, null, "the loading state shows at once");
  await cx.settle();
  assert.equal(held.get("/repo/c")?.length, 1, "C's read started without waiting for A's");
  await release("/repo/a", cx);
  assert.equal(v.board, null, "A's board is never shown");
  await release("/repo/c", cx);
  assert.equal(v.board.repo, "/repo/c");
  const words = text(v.render());
  assert.ok(words.includes("c change"));
  assert.ok(!words.includes("a change"), "A's cards are not shown again");
});
