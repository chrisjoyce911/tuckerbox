// Reading the board: `tuckerbox board --json` on the space's machine, or, with
// the dev flag, the fixtures next to this file. The board reads nothing else
// (design: the mod calls the CLI, never the files).
import { exec, readTextFile, appHome, props, spaces, panes, focused } from "herdr";
import { parseBoard, normaliseBoard, dirFromStack, resolveWhere } from "./model.js";

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

// Where the board reads, for this copy (`self`: its pane id, if known).
export function where(self = null) {
  return resolveWhere({ props: props() ?? {}, spaces: spaces(), panes: panes(), focused: focused(), self: self ?? props()?.pane ?? null });
}

async function tuckerbox(args, { cwd, on }) {
  try {
    const r = await exec("tuckerbox", args, { cwd: cwd || undefined, on, timeout: 20000 });
    return parseBoard(r.stdout ?? "", r.stderr ?? "", r.code ?? 0, { on });
  } catch (e) {
    const msg = String(e?.message ?? e);
    // exec rejects when the program can't start: tuckerbox isn't on that machine's PATH.
    return { error: { code: "unreachable", message: msg } };
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
