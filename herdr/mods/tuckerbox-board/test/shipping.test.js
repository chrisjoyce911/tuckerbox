import { test } from "node:test";
import assert from "node:assert/strict";
import { normaliseBoard, isActive, stateLook } from "../model.js";
import { detailActions, invocation, commandLine } from "../actions.js";

const card = {change:"add-login",state:"ready",column:"ready",approval:{state:"approved"},mr:{link:"https://github.com/example/shop/pull/12",number:12,state:"open"},ci:{state:"green",commit:"abc123"},ship_round:2,max_rounds:3};
test("Ready mr cards offer Resume and Open merge request, with no merge action",()=>{
 const board=normaliseBoard({ship:"mr",cards:[card]});const c=board.cards[0];
 assert.ok(detailActions(c,board).includes("resume"));assert.ok(detailActions(c,board).includes("mr"));assert.ok(!detailActions(c,board).includes("merge"));
 assert.deepEqual(commandLine("resume",c.change),["tuckerbox","resume","add-login"]);
 const inv=invocation("mr",c);assert.equal(inv.place,"browser");assert.equal(inv.argv,null);assert.equal(inv.url,card.mr.link);
 assert.equal(c.ci.state,"green");assert.equal(c.ship_round,2);assert.equal(c.max_rounds,3);
 assert.ok(!detailActions(c,{...board,ship:"merge"}).includes("resume"));
});
test("shipping holds the edit gate and merged cards retain the MR link action",()=>{
 assert.equal(isActive({...card,state:"shipping"}),true);assert.equal(stateLook({...card,state:"shipping"}).word,"shipping");
 assert.deepEqual(detailActions({...card,state:"merged",column:"done"},{ship:"merge"}),["mr"]);
 assert.equal(stateLook({...card,state:"merged"}).word,"merged");
});
test("browser actions reject non-HTTPS or credential-bearing links",()=>{
 for(const link of ["file:///tmp/x","javascript:alert(1)","https://secret@host/path"]){assert.throws(()=>invocation("mr",{...card,mr:{link}}));}
});
