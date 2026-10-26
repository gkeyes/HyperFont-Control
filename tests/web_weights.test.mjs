import test from 'node:test';
import assert from 'node:assert/strict';
import { changeWeightMode } from '../module/webroot/weights.js';

const defaults = () => Object.fromEntries(Array.from({length: 9}, (_, i) => [(i + 1) * 100, (i + 1) * 100]));
test('a saved continuous profile retains its prior custom mapping across reload', () => {
  let slot = {mode:'variable', familyMode:'instances', weightMap:{...defaults(),400:500}};
  changeWeightMode(slot,'continuous',defaults);
  assert.equal(slot.weightMap,undefined);
  slot = JSON.parse(JSON.stringify(slot));
  changeWeightMode(slot,'instances',defaults);
  assert.equal(slot.weightMap[400],500);
  assert.equal(slot.weightMap[300],300);
});
test('switching an existing instances profile does not reset its mapping', () => {
  const slot={familyMode:'instances',weightMap:{...defaults(),400:450}};
  changeWeightMode(slot,'instances',defaults);
  assert.equal(slot.weightMap[400],450);
});

import { shiftWeightMap, weightMapProblems } from '../module/webroot/weights.js';
const nine = Array.from({length: 9}, (_, i) => (i + 1) * 100);
test('a uniform shift moves every step and caps at the axis', () => {
  const out = shiftWeightMap(defaults(), 50, {min: 100, max: 900});
  assert.equal(out[100], 150); assert.equal(out[800], 850); assert.equal(out[900], 900);
  const down = shiftWeightMap(defaults(), -150, {min: 100, max: 900});
  assert.equal(down[100], 100); assert.equal(down[200], 100); assert.equal(down[900], 750);
});
test('problems skip out-of-range steps and blame both sides of a drop', () => {
  const p = weightMapProblems({...defaults(), 800: 950}, nine, {min: 100, max: 900});
  assert.match(p[800], /超出字体范围/); assert.equal(p[900], '');
  const q = weightMapProblems({...defaults(), 900: 750}, nine, {min: 100, max: 900});
  assert.match(q[900], /小于 800 档/); assert.match(q[800], /大于 900 档/); assert.equal(q[700], '');
});
