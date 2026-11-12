// Running an action. Actions are wired in add-herdr-board 6.4 to 6.6; until
// then a click builds the command and the board shows it without running it.
import { shown } from "./actions.js";

export async function perform(inv) {
  const what = inv.argv ? shown(inv.argv) : `${inv.action} (Herdr view)`;
  console.log(`tuckerbox-board: not wired yet: ${what}${inv.on ? ` on ${inv.on}` : ""}${inv.cwd ? ` in ${inv.cwd}` : ""}`);
  return { ok: false, wired: false, message: `Not wired yet: ${what}` };
}
