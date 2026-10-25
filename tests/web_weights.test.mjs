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
