// A change's step settings in card detail (settings-board): what each step
// runs with and why, and the pickers that change it through `tuckerbox set`.
// No Herdr or gpui imports: node tests it (test/settings.test.js).
import { isActive } from "./model.js";

// The step keys a picker may change, in display order.
export const PICK_KEYS = ["agent", "model", "effort", "permissions"];

// The picker entry that removes the change's own value.
export const REPO_DEFAULT = "repository default";

// What an empty value means for each key.
const EMPTY = { agent: "none", model: "agent default", effort: "agent default", permissions: "no profile" };

export const valueText = (key, s) => s?.value || EMPTY[key] || "";

// Pickers are offered on an active change's card that no run holds: not
// while a run is queued, running or shipping (it holds the change lock
// tuckerbox set takes), and never on archived cards.
export function canPick(card) {
  return Boolean(card) && !isActive(card) && card.column !== "done" && !card.archived;
}

// The values the allow-list lets a change pick for key on a step whose
// effective agent is agent. Empty: no picker for that key.
export function allowedValues(choices, key, agent) {
  const c = choices ?? {};
  switch (key) {
    case "agent": return c.agents ?? [];
    case "model": return c.models?.[agent] ?? [];
    case "effort": return c.efforts ?? [];
    case "permissions": return c.permissions ?? [];
    default: return [];
  }
}

// One row per workflow step, in the card's order: { step, started, keys }
// where each key is { key, setting, text, source, options }. `options` is
// null when the key has no picker (a run holds the change, or the
// allow-list permits nothing for it), else the allowed values followed by
// REPO_DEFAULT (null in `value`).
export function settingsRows(card, detail) {
  const pick = canPick(card);
  const choices = detail?.choices ?? null;
  return (card?.settings ?? []).map((s) => ({
    step: s.step,
    started: Boolean(s.started),
    keys: PICK_KEYS.map((key) => {
      const allowed = pick ? allowedValues(choices, key, s.agent?.value ?? "") : [];
      return {
        key,
        setting: s[key],
        text: valueText(key, s[key]),
        source: s[key]?.source ?? "default",
        options: allowed.length ? [...allowed.map((v) => ({ label: v, value: v })), { label: REPO_DEFAULT, value: null }] : null,
      };
    }),
  }));
}

// Whether picking key on this card needs the person to confirm first: the
// change is approved and key is one an approval covers (agent,
// permissions). model, effort and machines never ask.
export function voidsApproval(card, detail, key) {
  const keys = detail?.choices?.voids_approval ?? [];
  return card?.approval?.state === "approved" && keys.includes(key);
}

// What a pick does: { noop } when it would change nothing, else
// { extra, confirm } where `extra` is what the "set" action takes and
// `confirm` is the question to ask first, or "" to run at once. value
// null is REPO_DEFAULT.
export function choose(card, detail, step, key, value) {
  const s = (card?.settings ?? []).find((x) => x.step === step)?.[key];
  const own = s?.source === "change";
  if (value === null ? !own : own && s.value === value) return { noop: true };
  const extra = { step, key, value };
  if (!voidsApproval(card, detail, key)) return { extra, confirm: "" };
  const what = value === null ? `back to the repository's ${key}` : `to ${value}`;
  return {
    extra,
    confirm: `Changing the ${step} step's ${key} ${what} voids this change's approval: the card goes back to Proposed with approval stale until someone approves it again.`,
  };
}
