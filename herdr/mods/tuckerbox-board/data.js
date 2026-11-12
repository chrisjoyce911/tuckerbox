// Reading the board: `tuckerbox board --json` on the space's machine, or, with
// the dev flag, the fixtures next to this file. The board reads nothing else
// (design: the mod calls the CLI, never the files).
import { exec, readTextFile, appHome, props, spaces, panes, focused, machines } from "herdr";
import { parseBoard, normaliseBoard, dirFromStack, repoCandidates, pickRepo, otherMachines, sharedRepo, mergeBoards, execFailure } from "./model.js";

export const MOD = "tuckerbox-board";

// This mod's folder: a module has no import.meta.url here, but its stack names
// the file. Installed copies fall back to the app home's mods folder.
function modDir() {
  return dirFromStack(String(new Error().stack), "data.js") ?? `${appHome()}/mods/${MOD}`;
}

let devFile = undefined; // dev.json's contents once read: { fixtures: true | "<folder>" }

// The dev flag: props { fixtures: true | "<folder>" } (tab.new / pane.split
// with props), else dev.json beside main.js (not committed), else off.
export async function fixturesFolder() {
  const p = props()?.fixtures;
  if (devFile === undefined) {
    try { devFile = JSON.parse((await readTextFile(`${modDir()}/dev.json`)) ?? "null"); } catch { devFile = null; }
  }
  const flag = p ?? devFile?.fixtures;
  if (!flag) return null;
  return typeof flag === "string" ? flag : `${modDir()}/fixtures`;
}

async function readFixture(folder, name) {
  const text = await readTextFile(`${folder}/${name}`);
  if (text == null) return { error: { code: "failed", message: `fixture ${folder}/${name} not found` } };
  try { return { ...normaliseBoard(JSON.parse(text), { on: null }), fixtures: true }; } catch (e) {
    return { error: { code: "failed", message: `fixture ${name}: ${e?.message ?? e}` } };
  }
}

// The folders this copy looks in for its repository (`self`: its pane id, if known).
export function candidates(self = null) {
  return repoCandidates({ props: props() ?? {}, spaces: spaces(), panes: panes(), focused: focused(), self: self ?? props()?.pane ?? null });
}

// What a folder is, on a machine: the git checkout it is in and whether that
// has .tuckerbox/config.toml. Kept a while, shared by every copy (the board,
// the drawer), so locating every second costs nothing between looks.
const PROBE_MS = { yes: 30000, no: 10000 };
const probes = new Map(); // `${on}\0${cwd}` -> { at, ttl, result: Promise }

async function look(cwd, on) {
  try {
    const r = await exec("git", ["rev-parse", "--show-toplevel"], { cwd, on, timeout: 5000 });
    if ((r.code ?? 1) !== 0) return { root: null, config: false };
    const root = String(r.stdout ?? "").trim();
    if (!root) return { root: null, config: false };
    const text = await readTextFile(`${root}/.tuckerbox/config.toml`, { on });
    return { root, config: text != null };
  } catch {
    return null; // no git there, the folder is gone, or the machine is unreachable
  }
}

export function probe(cwd, on, now = Date.now()) {
  const key = `${on ?? ""}\0${cwd}`;
  const hit = probes.get(key);
  if (hit && now - hit.at < hit.ttl) return hit.result;
  const entry = { at: now, ttl: PROBE_MS.no, result: null };
  entry.result = look(cwd, on).then((r) => { entry.ttl = r?.config ? PROBE_MS.yes : PROBE_MS.no; return r; });
  probes.set(key, entry);
  return entry.result;
}

// Where the board reads, for this copy: { found, root, cwd, on, space }.
export async function locate(self = null) {
  const c = candidates(self);
  const found = await Promise.all(c.cwds.map((cwd) => probe(cwd, c.on)));
  return pickRepo(c, found);
}

async function tuckerbox(args, { cwd, on }, timeout = 20000) {
  try {
    const r = await exec("tuckerbox", args, { cwd: cwd || undefined, on, timeout });
    return parseBoard(r.stdout ?? "", r.stderr ?? "", r.code ?? 0, { on });
  } catch (e) {
    const msg = String(e?.message ?? e);
    // exec rejects when the program can't start (tuckerbox isn't on that
    // machine's PATH) or the machine can't be reached.
    return { error: { code: execFailure(msg), message: msg } };
  }
}

export async function loadBoard(at) {
  const fx = await fixturesFolder();
  if (fx) return readFixture(fx, "board.json");
  return tuckerbox(["board", "--json"], at);
}

export async function loadDetail(change, at) {
  const fx = await fixturesFolder();
  if (fx) return readFixture(fx, "detail.json");
  return tuckerbox(["board", "--json", "--change", change], at);
}

// The board from every connected machine (herdr-board: Runs from every
// machine): the space's machine's board, then each other machine's checkout of
// the same repository, found by its key (`tuckerbox board --json --repo`), merged
// by change id. Each card keeps its machine (`on`) and checkout (`path`), so its
// actions run there. A machine that can't be asked is named, not fatal.
// Another machine gets less time, so one slow machine can't hold up the board long.
const OTHER_MS = 8000;

export async function loadAll(at) {
  const own = await loadBoard(at);
  if (own.error || own.fixtures || !sharedRepo(own)) return own;
  let list = [];
  try { list = otherMachines(machines(), at.on ?? null); } catch { list = []; }
  const others = await Promise.all(list.map(async (m) => ({
    machine: m.name,
    board: m.reachable
      ? await tuckerbox(["board", "--json", "--repo", own.repo], { cwd: "", on: m.name }, OTHER_MS)
      : { error: { code: "unreachable", message: "offline" } },
  })));
  return mergeBoards(own, others);
}
