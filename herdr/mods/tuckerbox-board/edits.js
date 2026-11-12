// Edits from the board (6.5): a pane runs `tuckerbox edit`; when it closes,
// the board runs `tuckerbox validate --json` and keeps the result in memory,
// for the card and its detail. No Herdr imports: node tests it.
//
// Herdr's run() returns nothing, so the board can't be told which pane it
// opened. It notes the panes before, takes the first new terminal pane that
// appears within OPEN_MS as the edit's, and watches for it to go. If none
// appears, the edit is "unwatched": the card says it wasn't validated since
// the edit and offers Validate.

export const OPEN_MS = 6000;

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

export function startEdit(card, file, panesNow, now = Date.now()) {
  const rec = { state: "opening", pane: null, before: new Set((panesNow ?? []).map((p) => p.pane)), started: now, file, result: null, error: null };
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

// The next state of one record, from the live panes: pure.
export function step(rec, panesNow, now = Date.now()) {
  const panes = panesNow ?? [];
  if (rec.state === "opening") {
    const fresh = panes.find((p) => p.terminal && !rec.before.has(p.pane));
    if (fresh) return { ...rec, state: "editing", pane: fresh.pane };
    if (now - rec.started > OPEN_MS) return { ...rec, state: "unwatched" };
    return rec;
  }
  if (rec.state === "editing" && !panes.some((p) => p.pane === rec.pane)) return { ...rec, state: "closed" };
  return rec;
}

// Moves every record on; returns the cards (by key) whose pane just closed,
// for the caller to validate. Each closes once: it becomes "validating".
export function tick(panesNow, now = Date.now()) {
  const closed = [];
  for (const [key, rec] of records) {
    const next = step(rec, panesNow, now);
    if (next.state === "closed") { records.set(key, { ...next, state: "validating" }); closed.push(key); }
    else if (next !== rec) records.set(key, next);
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
    case "unwatched": return { text: "Not validated since the edit: the board couldn't see the editor's pane", tone: "blocked", validate: true };
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
