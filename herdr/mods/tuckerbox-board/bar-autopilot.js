// Autopilot from the top bar's menu (add-herdr-topbar: Autopilot from the
// bar). The menu shows whether tuckerbox autopilot runs for the shown
// space's repository, from the board document's `autopilot`, and offers
// Start or Stop. Start runs `tuckerbox autopilot` in a herdr-core terminal
// named for the repository on the space's machine; Stop sends that terminal
// Ctrl-C, which autopilot handles by finishing its pass and exiting. The bar
// writes nothing itself: herdr-core's unique terminal names and autopilot's
// own lock keep it to one autopilot however often Start is chosen.
import { hc, notify, theme } from "herdr";
import { div } from "gpui-kit";
import { v_flex } from "gpui-base";
import { candidates, locate, loadBoard } from "./data.js";
import { candidatesKey } from "./model.js";

// The herdr-core name of a repository's autopilot terminal: "tb-ap-" and the
// first 12 hex digits of a SHA-256 of the repository's identity (the board's
// repo: its normalised remote, or its path without one), as agent terminals
// are named (add-live-agents).
export function autopilotName(repo) {
  return "tb-ap-" + sha256Hex(utf8(String(repo ?? ""))).slice(0, 12);
}

const basename = (p) => String(p ?? "").replace(/\\/g, "/").replace(/\/+$/, "").split("/").pop() ?? "";

// What the menu shows for autopilot, or null when it has no Autopilot item
// (approval off: autopilot refuses to run without it). `{ status, detail,
// actions }`, actions a list of "start" or "stop". An autopilot in another
// terminal (a person's shell) shows as running with no Stop: the bar only
// stops one it can name.
export function autopilotMenu(board) {
  if (!board || board.error || board.fixtures || !board.require_approval) return null;
  const a = board.autopilot ?? {};
  if (!a.running) return { status: "Autopilot is off", detail: "", actions: ["start"] };
  const where = [a.pid ? `pid ${a.pid}` : "", a.machine ? `on ${a.machine}` : ""].filter(Boolean).join(" ");
  const own = Boolean(a.terminal) && a.terminal === autopilotName(board.repo);
  return {
    status: "Autopilot runs",
    detail: own ? [where, `in terminal ${a.terminal}`].filter(Boolean).join(", ") : [where, "started outside the bar"].filter(Boolean).join(", "),
    actions: own ? ["stop"] : [],
  };
}

export const AUTOPILOT_LABELS = { start: "Start autopilot", stop: "Stop autopilot" };

// The command the terminal runs: tuckerbox autopilot, told its terminal's
// name (TUCKERBOX_TERMINAL, which it records in its marker) so the menu can
// tell it from one in a person's shell. herdr-core puts no name in the
// environment, and hc run has no option for one, so the command sets it:
// env on Unix, PowerShell on Windows (the board document's os).
export function autopilotArgv(name, os = "") {
  if (os === "windows") {
    return ["powershell", "-NoProfile", "-NonInteractive", "-Command", `$env:TUCKERBOX_TERMINAL='${name}'; tuckerbox autopilot; exit $LASTEXITCODE`];
  }
  return ["env", `TUCKERBOX_TERMINAL=${name}`, "tuckerbox", "autopilot"];
}

// The `hc` arguments that start autopilot for the board's repository on its
// machine (`board.on`, null for this one), in its checkout, in the space's
// autopilot tab. --bg: it comes back at once and autopilot keeps running.
// --no-parent: it belongs to no terminal whose end would end it.
export function startArgs(board) {
  const name = autopilotName(board.repo);
  return [
    "run", "--json", "--bg", "--name", name,
    ...(board.on ? ["--on", board.on] : []),
    "--cwd", board.path,
    "--label", `herdr.space=${basename(board.path)}`, "--label", "herdr.tab=autopilot",
    "--no-parent",
    "--", ...autopilotArgv(name, board.os),
  ];
}

// The `hc` arguments that send the repository's autopilot terminal Ctrl-C.
// A terminal on another machine is addressed as <machine>/<name>.
export function stopArgs(board) {
  const name = autopilotName(board.repo);
  return ["keys", "--json", board.on ? `${board.on}/${name}` : name, "ctrl+c"];
}

// How to run autopilot by hand, when herdr-core isn't there to run it.
export function manualHint(board) {
  const where = board.on ? ` on ${board.on}` : "";
  return `herdr-core (hc) isn't available${where}: run \`tuckerbox autopilot\` in a terminal in ${board.path}`;
}

const MISSING = /not found|no such file|ENOENT|cannot find|is not recognized|not installed/i;

function hcError(text) {
  const t = String(text ?? "").trim();
  try {
    const doc = JSON.parse(t);
    if (doc?.error) return [doc.error.message || doc.error.kind || "hc failed", doc.error.hint ? `(${doc.error.hint})` : ""].filter(Boolean).join(" ");
  } catch { /* not JSON */ }
  return t;
}

async function call(args, board) {
  let r;
  try {
    r = await hc(args);
  } catch (e) {
    const message = String(e?.message ?? e);
    return MISSING.test(message) ? { ok: false, message: manualHint(board), hint: true } : { ok: false, message };
  }
  if ((r?.code ?? 0) === 0) return { ok: true, message: "" };
  const message = hcError(r?.stderr) || hcError(r?.stdout) || `hc exited with code ${r?.code}`;
  return MISSING.test(message) && !/terminal/i.test(message) ? { ok: false, message: manualHint(board), hint: true } : { ok: false, message };
}

// Start and Stop. Each resolves with { ok, message, hint }: hint is true when
// the message says how to run autopilot by hand.
export async function startAutopilot(board) {
  const r = await call(startArgs(board), board);
  return r.ok ? { ok: true, message: `Autopilot starting in terminal ${autopilotName(board.repo)}` } : r;
}

export async function stopAutopilot(board) {
  const r = await call(stopArgs(board), board);
  return r.ok ? { ok: true, message: "Autopilot stops after its pass" } : r;
}

// SHA-256 (FIPS 180-4) of bytes, as lowercase hex. The mod's runtime has no
// crypto module, and a name needs only a few bytes hashed.
const K = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

export function sha256Hex(bytes) {
  const n = bytes.length;
  const len = (((n + 9 + 63) >> 6) << 6);
  const m = new Uint8Array(len);
  m.set(bytes);
  m[n] = 0x80;
  const bits = n * 8;
  for (let i = 0; i < 8; i++) m[len - 1 - i] = Math.floor(bits / 2 ** (8 * i)) & 0xff;
  const h = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
  const w = new Array(64);
  const rotr = (x, r) => (x >>> r) | (x << (32 - r));
  for (let off = 0; off < len; off += 64) {
    for (let i = 0; i < 16; i++) {
      const j = off + i * 4;
      w[i] = ((m[j] << 24) | (m[j + 1] << 16) | (m[j + 2] << 8) | m[j + 3]) >>> 0;
    }
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const t1 = (hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i]) >>> 0;
      const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) >>> 0;
      hh = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
    }
    h[0] = (h[0] + a) >>> 0; h[1] = (h[1] + b) >>> 0; h[2] = (h[2] + c) >>> 0; h[3] = (h[3] + d) >>> 0;
    h[4] = (h[4] + e) >>> 0; h[5] = (h[5] + f) >>> 0; h[6] = (h[6] + g) >>> 0; h[7] = (h[7] + hh) >>> 0;
  }
  return h.map((x) => x.toString(16).padStart(8, "0")).join("");
}

function utf8(s) {
  const out = [];
  for (const ch of s) {
    let c = ch.codePointAt(0);
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
  }
  return Uint8Array.from(out);
}

// The menu's Autopilot section for the shown space: a status line and Start
// or Stop, as the bar menu's `item(id, label, action)` draws them (choosing
// one closes the menu). It reads the shown space's board itself (the
// document's autopilot is for that board's machine) and keeps it a short
// while, shared by every menu copy; `cx` re-renders the menu when it lands.
// Draws nothing until the board is read, or with approval off.
const READ_MS = 2000;
const cache = { key: null, board: null, at: 0, loading: false };

export function autopilotBoard() { return cache.board; }
export function forgetAutopilot() { cache.at = 0; }

function load(c, key, cx) {
  cache.loading = true;
  cx.spawn(async (cx) => {
    let board = null;
    try {
      const at = await locate(null, c);
      if (at?.found) board = await loadBoard(at);
    } catch { /* an unreachable machine has no autopilot to show */ }
    cache.loading = false;
    if (cache.key !== key) return;
    cache.board = board; cache.at = Date.now();
    cx.notify();
  });
}

// Start or Stop from the menu: the outcome is a toast, and the next read
// shows the new state.
async function act(fn, board) {
  const r = await fn(board);
  forgetAutopilot();
  notify({ title: r.ok ? r.message : "Autopilot", ...(r.ok ? {} : { body: r.message }), level: r.ok ? "info" : (r.hint ? "warning" : "error"), timeout: r.ok ? 4000 : 10000 });
  return r;
}

export function renderAutopilot(item, cx) {
  const c = candidates(null), key = candidatesKey(c);
  if (key !== cache.key) { cache.key = key; cache.board = null; cache.at = 0; }
  if (!cache.loading && Date.now() - cache.at >= READ_MS) load(c, key, cx);
  const board = cache.board;
  const menu = autopilotMenu(board);
  if (!menu) return div();
  const line = [menu.status, menu.detail].filter(Boolean).join(" · ");
  let el = v_flex().id("bar-autopilot").mt(4)
    .child(div().id("bar-autopilot-status").px(8).py(3).text_size(11).text_color(theme()?.dim).child(line));
  for (const a of menu.actions) {
    el = el.child(item(`bar-autopilot-${a}`, AUTOPILOT_LABELS[a], () => { act(a === "start" ? startAutopilot : stopAutopilot, board); }));
  }
  return el;
}

// How much taller the menu is with the Autopilot section, for the popup's
// height (the bar knows require_approval from its own read).
export const AUTOPILOT_MENU_PX = 56;
