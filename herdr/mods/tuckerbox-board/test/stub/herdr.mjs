// The "herdr" module for tests: each function reads `state`, which a test sets.
export const state = {};
export function reset() {
  Object.assign(state, {
    props: {}, machines: [], spaces: [], panes: [], focused: null, theme: {}, placement: null,
    exec: async () => ({ code: 1, stdout: "", stderr: "not stubbed" }),
    readTextFile: async () => null,
    runs: [], overlays: [], notices: [],
  });
}
reset();
export const exec = (...a) => state.exec(...a);
export const readTextFile = (...a) => state.readTextFile(...a);
export const appHome = () => "/herdr-home";
export const props = () => state.props;
export const spaces = () => state.spaces;
export const machines = () => state.machines;
export const panes = () => state.panes;
export const focused = () => state.focused;
export const theme = () => state.theme;
export const placement = () => state.placement;
export const run = (command, args) => { state.runs.push({ command, args }); return null; };
export const notify = (n) => { state.notices.push(n); return null; };
export const openOverlay = (spec) => { state.overlays.push(spec); return "overlay-1"; };
export const closeOverlay = () => null;
