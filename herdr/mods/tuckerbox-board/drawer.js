// The drawer (`view: "drawer"`): a small floating peek drawer that
// `tuckerbox herdr install --drawer` adds to the person's layout. It draws
// nothing unless the shown space has a Tuckerbox repository (a git checkout
// with .tuckerbox/config.toml, found as the board finds it). Then it shows the
// running and needs-you counts from `tuckerbox board --json` every 30 s, and a
// click opens or hides the board window.
import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { theme, placement, run } from "herdr";
import { MOD, loadBoard, locate, candidates } from "./data.js";
import { counts, candidatesKey, drawerRows, DRAWER_POLL_MS } from "./model.js";
import { toggleCall } from "./actions.js";
import { dot, SPINNER } from "./ui.js";

export class DrawerView extends View {
  init(p, cx) {
    // `at`, `counts` and `repo` may be handed in (a test harness); otherwise they are read.
    this.at = p?.at ?? null; // { found, root, on, ... }
    this.counts = p?.counts ?? null;
    this.repo = p?.repo ?? "";
    this.last = this.at ? Date.now() : 0;
    // want: the candidates key (space, machine, folders) the drawer shows;
    // loading: the keys with a read running (board-view: Stale reads are dropped).
    this.want = this.at ? candidatesKey(candidates(null)) : null;
    this.loading = new Set();
    cx.timer.after(30, (cx) => this.refresh(cx));
    cx.timer.every(1000, (cx) => this.tick(cx));
  }

  // Follow a space switch at once; otherwise read the counts every 30 s.
  tick(cx) {
    const key = candidatesKey(candidates(null));
    if (key !== this.want) this.follow(key, cx);
    if (!this.last || Date.now() - this.last >= DRAWER_POLL_MS) this.refresh(cx);
  }

  // The space or its folders changed: stop showing the old result now.
  follow(key, cx) {
    this.want = key;
    this.at = null;
    this.counts = null;
    this.repo = "";
    this.last = 0;
    cx.notify();
  }

  refresh(cx) {
    const c = candidates(null);
    const key = candidatesKey(c);
    if (key !== this.want) this.follow(key, cx);
    if (this.loading.has(key)) return;
    this.loading.add(key);
    cx.spawn(async (cx) => {
      let at = null, n = null;
      try {
        at = await locate(null, c);
        if (at.found) {
          const b = await loadBoard(at);
          n = b.error ? { error: b.error.message } : counts(b);
        }
      } catch { at = null; n = null; }
      this.loading.delete(key);
      if (key !== this.want) return; // a read for a space no longer shown: dropped
      this.at = at;
      this.counts = n;
      this.repo = at?.root ? at.root.replace(/\\/g, "/").split("/").filter(Boolean).pop() : "";
      this.last = Date.now();
      cx.notify();
    });
  }

  render() {
    // No Tuckerbox config in the shown space: nothing at all.
    if (!this.at?.found) return div();
    const t = theme();
    const pl = placement() ?? { vertical: true, edge: "right" };
    const right = pl.edge === "right";
    const rows = drawerRows(this.counts, this.repo);
    const glyph = (r) => r.kind === "running" ? svg(SPINNER).size(11).text_color(t.working).spin(900)
      : r.kind === "needs" ? dot(t.blocked, 8)
        : r.kind === "error" ? div().text_size(12).text_color(t.blocked).child("!")
          : div().text_size(12).font_semibold().text_color(t.accent).child("T");
    const row = (r) => {
      const el = h_flex().id(`drawer-${r.kind}`).items_center().gap(8).px(10).h(26)
        .child(h_flex().w(34).flex_none().items_center().justify_center().gap(3)
          .child(glyph(r)).when(r.count != null, (e) => e.child(div().text_size(11).text_color(t.foreground).child(String(r.count)))))
        .child(div().text_size(12).text_color(r.tone ? t[r.tone] : t.muted).truncate().max_w(180).child(r.text));
      // The edge side shows while it peeks: mirror rows on the right edge.
      return right ? el.flex_row_reverse() : el;
    };
    const box = pl.vertical ? v_flex().py(4) : h_flex().px(4);
    return box.id("tuckerbox-drawer").cursor_pointer()
      .tooltip("Open or hide the Tuckerbox board")
      .on_click(() => { const c = toggleCall(MOD); run(c.command, c.args); })
      .children(rows.map(row));
  }
}
