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
    cx.timer.after(30, (cx) => this.refresh(cx));
    cx.timer.every(1000, (cx) => {
      // Follow a space switch at once; otherwise read the counts every 30 s.
      const key = candidatesKey(candidates(null));
      if (key !== this.where) { this.where = key; this.last = 0; }
      if (!this.last || Date.now() - this.last >= DRAWER_POLL_MS) this.refresh(cx);
    });
  }

  refresh(cx) {
    if (this.loading) return;
    this.loading = true;
    cx.spawn(async (cx) => {
      try {
        const at = await locate(null);
        let n = null;
        if (at.found) {
          const b = await loadBoard(at);
          n = b.error ? { error: b.error.message } : counts(b);
        }
        this.at = at;
        this.counts = n;
        this.repo = at.root ? at.root.replace(/\\/g, "/").split("/").filter(Boolean).pop() : "";
      } catch { this.at = null; this.counts = null; }
      this.loading = false;
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
