// A change's proposal in a pane beside the board (Open proposal). It reads
// the text through `tuckerbox board --json --change <id>`, on the card's
// machine, like the detail dialog, and re-reads it every 5 s while shown.
import { View, div } from "gpui-kit";
import { v_flex, h_flex, TextView } from "gpui-base";
import { theme, props, spaces, panes } from "herdr";
import { loadDetail, MOD } from "./data.js";
import { due, isShown, cardAt } from "./model.js";
import { tiny } from "./ui.js";

export class ProposalView extends View {
  init(p, cx) {
    const pr = props() ?? p ?? {};
    this.change = pr.change ?? "";
    this.at = cardAt(pr.card, pr.board);
    this.text = pr.text ?? null;
    this.error = null;
    this.last = this.text == null ? 0 : Date.now();
    cx.timer.after(30, (cx) => this.load(cx));
    cx.timer.every(1000, (cx) => {
      const shown = isShown({ overlay: null, self: props()?.pane ?? null, spaces: spaces(), panes: panes(), mod: MOD });
      if (due(this.last, Date.now(), shown)) this.load(cx);
    });
  }

  load(cx) {
    if (this.loading) return;
    this.loading = true;
    cx.spawn(async (cx) => {
      try {
        const d = await loadDetail(this.change, this.at);
        if (d.error) this.error = d.error.message;
        else if (d.detail?.change !== this.change) this.error = `No detail for ${this.change}.`;
        else { this.text = d.detail.proposal; this.error = null; }
      } catch (e) { this.error = String(e?.message ?? e); }
      this.loading = false;
      this.last = Date.now();
      cx.notify();
    });
  }

  render() {
    const tm = theme();
    return v_flex().id("proposal-view").size_full().bg(tm.background).text_color(tm.foreground)
      .child(h_flex().items_center().gap(8).px(14).h(36).flex_none().border_b(1).border_color(tm.border)
        .child(div().text_size(12.5).font_semibold().child("Proposal"))
        .child(div().flex_1().min_w(0).truncate().text_size(11.5).text_color(tm.dim).child(this.change))
        .child(tiny("reload", "Reload", (_e, cx) => { this.last = 0; this.load(cx); })))
      .when(Boolean(this.error), (el) => el.child(div().p(14).text_size(12).text_color(tm.blocked).child(this.error)))
      .child(v_flex().id("proposal-scroll").flex_1().min_h(0).overflow_y_scrollbar().p(14)
        .child(this.text != null ? TextView.markdown("proposal-md", this.text).selectable(true).text_size(13)
          : div().text_size(12).text_color(tm.dim).child("Reading the proposal…")));
  }
}
