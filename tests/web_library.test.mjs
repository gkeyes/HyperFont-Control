import test from 'node:test';
import assert from 'node:assert/strict';
import { faceFamily, faceOptionLabel, fileStem, groupFonts } from '../module/webroot/library.js';

const font = (label, name, extra = {}) => ({ label, faces: [{ name, weight: 400, axes: [], ...extra }] });

test('family drops trailing style words', () => {
  assert.equal(faceFamily({ name: 'HYQiHeiClassic Regular' }), 'HYQiHeiClassic');
  assert.equal(faceFamily({ name: 'Noto Sans SC ExtraLight' }), 'Noto Sans SC');
  assert.equal(faceFamily({ name: 'MiSans VF' }), 'MiSans VF');
  assert.equal(faceFamily({ name: 'Bold' }), 'Bold');
});

test('groups by family and sorts file names numerically', () => {
  const groups = groupFonts([
    font('HYQiHeiClassic-105JF.ttf', 'HYQiHeiClassic Regular'),
    font('HYQiHeiClassic-25S.ttf', 'HYQiHeiClassic Regular'),
    font('HYQiHeiClassic-40S.ttf', 'HYQiHeiClassic Regular'),
    font('Inter.ttf', 'Inter Regular'),
  ]);
  assert.deepEqual(groups.map(g => [g.family, g.fonts.map(f => f.label)]), [
    ['HYQiHeiClassic', ['HYQiHeiClassic-25S.ttf', 'HYQiHeiClassic-40S.ttf', 'HYQiHeiClassic-105JF.ttf']],
    ['Inter', ['Inter.ttf']],
  ]);
});

test('option label names the file', () => {
  const f = font('HYQiHeiClassic-25S.ttf', 'HYQiHeiClassic Regular');
  assert.equal(fileStem(f.label), 'HYQiHeiClassic-25S');
  assert.equal(faceOptionLabel(f, f.faces[0]), 'HYQiHeiClassic-25S · 400');
  const ttc = { label: 'Pack.ttc', faces: [{ name: 'A Regular', weight: 400, axes: [] }, { name: 'A Bold', weight: 700, axes: [] }] };
  assert.equal(faceOptionLabel(ttc, ttc.faces[1]), 'Pack · A Bold · 700');
});

test('spreads sixteen same-weight files over nine steps by file name', async () => {
  const { spreadFamily } = await import('../module/webroot/library.js');
  const names = ['25S', '30S', '35S', '40S', '45S', '50S', '55S', '60S', '65S', '70S', '75S', '80S', '85S', '90S', '95S', '105JF'];
  const entries = names.reverse().map((n, i) => ({ asset: { id: n, label: `HY-${n}.ttf` }, face: { index: 0, weight: 400, italic: false } }));
  const weights = [100, 200, 300, 400, 500, 600, 700, 800, 900];
  assert.deepEqual(spreadFamily(entries, weights).map(r => `${r.weight}:${r.assetId}`), ['100:25S', '200:35S', '300:45S', '400:55S', '500:65S', '600:70S', '700:80S', '800:90S', '900:105JF']);
  const three = ['Light', 'Regular', 'Bold'].map((n, i) => ({ asset: { id: n, label: `${n}.ttf` }, face: { index: 0, weight: [300, 400, 700][i], italic: false } }));
  assert.deepEqual(spreadFamily(three, weights).map(r => `${r.weight}:${r.assetId}`), ['300:Light', '400:Regular', '700:Bold']);
});

test('spreads a few same-weight files evenly', async () => {
  const { spreadFamily } = await import('../module/webroot/library.js');
  const three = ['A', 'B', 'C'].map(n => ({ asset: { id: n, label: `${n}.ttf` }, face: { index: 0, weight: 400, italic: false } }));
  assert.deepEqual(spreadFamily(three, [100, 200, 300, 400, 500, 600, 700, 800, 900]).map(r => `${r.weight}:${r.assetId}`), ['100:A', '500:B', '900:C']);
});
