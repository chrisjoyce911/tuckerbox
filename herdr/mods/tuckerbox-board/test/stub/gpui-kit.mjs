import { element } from "./element.mjs";
export class View {}
export const div = () => element("div");
export const svg = (path) => element("svg", path);
