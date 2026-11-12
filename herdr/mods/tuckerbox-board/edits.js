// Edits from the board (6.5): a pane runs `tuckerbox edit`; when it closes,
// the board runs `tuckerbox validate --json` and keeps the result in memory,
// for the card and its detail. No Herdr imports: node tests it.
//
// Herdr's run() returns nothing, so the board can't be told which pane it
// opened. It notes the panes before and claims a new terminal pane as the
// edit's only when it is the edit's own: on the edit's machine, in the space
// it was opened in (when known), in the edit's folder, and claimed by no
// other edit. When no such pane appears within OPEN_MS, or more than one
// could be it, or one pane could belong to two edits, the edit is
// "unwatched": the card says it wasn't validated since the edit and offers
// Validate. Guessing would validate on the wrong pane closing.

export const OPEN_MS = 10000;

// Edit records by card: key -> { state, pane, before, started, file, result, error }.
//   opening     the pane was asked for, not seen yet
//   editing     its pane is open
//   validating  it closed; validate runs
//   unwatched   no pane was seen: Validate by hand
//   done        validated: result is { valid, errors } (or error)
// Module state, shared by every copy of the mod (the board and its dialogs).
const records = new Map();

export const editKey = (card) => JSON.stringify([card?.on ?? null, card?.path ?? "", card?.change ?? ""]);

export function editOf(card) {
  return records.get(editKey(card)) ?? null;
}

// `target` is where the editor's pane goes: `on` (its machine, null here),
// `space` (the space it opens in, or null when it gets a new space) and
// `folder` (where it starts).
export function startEdit(card, file, panesNow, now = Date.now(), target = {}) {
  const rec = {
    state: "opening", pane: null, before: new Set((panesNow ?? []).map((p) => p.pane)), started: now, file,
    on: target.on ?? null, space: target.space ?? null, folder: target.folder ?? "", result: null, error: null,
  };
  records.set(editKey(card), rec);
  return rec;
}

export function setValidation(card, v) {
  const rec = records.get(editKey(card)) ?? { file: "", before: new Set(), started: 0 };
  records.set(editKey(card), { ...rec, state: "done", pane: null, result: v.error ? null : v, error: v.error ?? null });
}

export function markValidating(card) {
  const rec = records.get(editKey(card));
  if (rec) records.set(editKey(card), { ...rec, state: "validating" });
}

// Remote pane ids carry their machine: "mini/p3". None: this computer.
export const machineOfPane = (id) => (String(id).includes("/") ? String(id).split("/")[0] : null);

const norm = (p) => String(p ?? "").replace(/\\/g, "/").replace(/\/+$/, "");
const inside = (p, root) => Boolean(p) && Boolean(root) && (norm(p) === norm(root) || norm(p).startsWith(`${norm(root)}/`));

// Could this pane be rec's editor? A new terminal pane on its machine, in its
// space when known, started in its folder.
export function candidate(rec, pane, spacesNow) {
  if (!pane.terminal || rec.before.has(pane.pane)) return false;
  if (machineOfPane(pane.pane) !== (rec.on ?? null)) return false;
  if (rec.space) {
    const space = (spacesNow ?? []).find((s) => s.id === rec.space);
    if (!space || !(space.tabs ?? []).some((t) => t.id === pane.tab)) return false;
  }
  return inside(pane.cwd, rec.folder);
}

// Moves every record on, from the live panes and spaces; returns the cards
// (by key) whose editor pane just closed, for the caller to validate. Each
// closes once: it becomes "validating".
export function tick(panesNow, now = Date.now(), spacesNow = []) {
  const panes = panesNow ?? [];
  const closed = [];
  const claimed = new Set([...records.values()].filter((r) => r.state === "editing").map((r) => r.pane));
  // Each opening edit's candidates, and how many edits want each pane.
  const opening = [...records].filter(([, r]) => r.state === "opening");
  const wants = new Map();
  const cands = new Map();
  for (const [key, rec] of opening) {
    const c = panes.filter((p) => !claimed.has(p.pane) && candidate(rec, p, spacesNow)).map((p) => p.pane);
    cands.set(key, c);
    for (const id of c) wants.set(id, (wants.get(id) ?? 0) + 1);
  }
  for (const [key, rec] of records) {
    if (rec.state === "opening") {
      const c = cands.get(key);
      if (c.length === 1 && wants.get(c[0]) === 1) records.set(key, { ...rec, state: "editing", pane: c[0] });
      else if (c.length > 1 || c.some((id) => wants.get(id) > 1)) records.set(key, { ...rec, state: "unwatched", why: "ambiguous" });
      else if (now - rec.started > OPEN_MS) records.set(key, { ...rec, state: "unwatched", why: "unseen" });
    } else if (rec.state === "editing" && !panes.some((p) => p.pane === rec.pane)) {
      records.set(key, { ...rec, state: "validating" });
      closed.push(key);
    }
  }
  return closed;
}

// The card's line about its last edit, or null: { text, tone, validate }.
// `validate` says whether to offer the Validate button.
export function editNote(rec) {
  if (!rec) return null;
  switch (rec.state) {
    case "opening": return { text: `Opening the editor for ${rec.file}…`, tone: "dim", validate: false };
    case "editing": return { text: `Editing ${rec.file}: validates when its pane closes`, tone: "dim", validate: false };
    case "validating": return { text: "Validating…", tone: "dim", validate: false };
    case "unwatched": return { text: rec.why === "ambiguous"
      ? "Not validated since the edit: the board couldn't tell which pane is the editor"
      : "Not validated since the edit: the board couldn't see the editor's pane", tone: "blocked", validate: true };
    case "done":
      if (rec.error) return { text: `Validation didn't run: ${rec.error}`, tone: "blocked", validate: true };
      return rec.result.valid
        ? { text: "Valid after the last edit", tone: "working", validate: true }
        : { text: ["Invalid after the last edit:", ...rec.result.errors].join("\n"), tone: "blocked", validate: true };
    default: return null;
  }
}

// For tests.
export function reset() {
  records.clear();
}

export function keyCard(key) {
  const [on, path, change] = JSON.parse(key);
  return { on, path, change };
}
