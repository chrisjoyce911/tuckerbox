// The Tuckerbox board in Herdr: OpenSpec changes as cards in columns derived
// by `tuckerbox board --json`. One mod, two views:
//   (no view)     the board (a window from mod.open, or a tab or pane)
//   view: "card"      one change's detail, as a dialog the board opens
//   view: "proposal"  a change's proposal, as a pane beside the board
//   view: "drawer"    the floating peek drawer (tuckerbox herdr install --drawer)
// Props: space (the space to follow), folder and machine (a docked board: read
// the board there; `on` is the older name for machine),
// fixtures (true or a folder: render fixtures/*.json instead of running tuckerbox).
import { View, div } from "gpui-kit";
import { props } from "herdr";
import { BoardView } from "./board.js";
import { CardView } from "./card.js";
import { ProposalView } from "./proposal.js";
import { DrawerView } from "./drawer.js";

export default class Main extends View {
  init(p, cx) {
    const Inner = { card: CardView, proposal: ProposalView, drawer: DrawerView }[props()?.view] ?? BoardView;
    this.inner = new Inner(p);
    this.inner.init?.(p, cx);
  }
  render(cx) {
    return this.inner ? this.inner.render(cx) : div().size_full();
  }
  close(cx) {
    return this.inner?.close?.(cx);
  }
}
