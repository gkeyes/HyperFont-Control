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

test('puts a few same-weight files on the steps nearest their weight', async () => {
  const { spreadFamily } = await import('../module/webroot/library.js');
  const three = ['A', 'B', 'C'].map(n => ({ asset: { id: n, label: `${n}.ttf` }, face: { index: 0, weight: 400, italic: false } }));
  assert.deepEqual(spreadFamily(three, [100, 200, 300, 400, 500, 600, 700, 800, 900]).map(r => `${r.weight}:${r.assetId}`), ['300:A', '400:B', '500:C']);
});

test('reads weights written in names', async () => {
  const { nameWeight } = await import('../module/webroot/library.js');
  assert.equal(nameWeight('NotoSansSC-SemiBold'), 600);
  assert.equal(nameWeight('MiSans-ExtraLight'), 200);
  assert.equal(nameWeight('Source Han Sans Extra Light'), 200);
  assert.equal(nameWeight('Hiragino W6'), 600);
  assert.equal(nameWeight('Font_300'), 300);
  assert.equal(nameWeight('HYQiHeiClassic-25S'), null);
  assert.equal(nameWeight('Highlight'), null);
});

test('keeps distinct declared weights when only some files collide', async () => {
  const { spreadFamily } = await import('../module/webroot/library.js');
  const f = (label, weight) => ({ asset: { id: label, label: `${label}.ttf` }, face: { index: 0, name: 'X Regular', weight, italic: false } });
  // Light and Bold declare their own weights; Medium and Black wrongly declare 400 but say so in the name.
  const refs = spreadFamily([f('X-Light', 300), f('X-Bold', 700), f('X-Regular', 400), f('X-Medium', 400), f('X-Black', 400)], [100, 200, 300, 400, 500, 600, 700, 800, 900]);
  const normal = refs.map(r => `${r.weight}:${r.assetId}${r.guess ? '?' : ''}`);
  assert.deepEqual(normal.filter(r => ['300', '700', '500', '900'].includes(r.split(':')[0])), ['300:X-Light', '500:X-Medium?', '700:X-Bold', '900:X-Black?']);
  assert.ok(normal.includes('400:X-Regular?'));
});

test('groups Download files by folder in natural order', async () => {
  const { groupDownloads } = await import('../module/webroot/library.js');
  const groups = groupDownloads([{ name: 'B-10.ttf', folder: 'fonts' }, { name: 'B-9.ttf', folder: 'fonts' }, { name: 'A.ttf' }, { name: 'C.ttf', folder: 'Abc' }]);
  assert.deepEqual(groups.map(g => [g.folder, g.files.map(f => f.name)]), [['', ['A.ttf']], ['Abc', ['C.ttf']], ['fonts', ['B-9.ttf', 'B-10.ttf']]]);
});

test('files sharing declared and name weights take the nearest free steps', async () => {
  const { spreadFamily } = await import('../module/webroot/library.js');
  const W = [100, 200, 300, 400, 500, 600, 700, 800, 900];
  const f = (label, weight, name = 'X Regular') => ({ asset: { id: label, label: `${label}.ttf` }, face: { index: 0, name, weight, italic: false } });
  const show = refs => refs.map(r => `${r.weight}:${r.assetId}${r.guess ? `?${r.guess}` : ''}`);
  assert.deepEqual(show(spreadFamily([f('Fam-Light', 300), f('Fam-Book', 400), f('Fam-Regular', 400), f('Fam-Bold', 700)], W)), ['300:Fam-Light', '400:Fam-Book?name', '500:Fam-Regular?order', '700:Fam-Bold']);
  assert.deepEqual(show(spreadFamily([f('A', 400, 'A'), f('B', 400, 'B')], W)), ['400:A?order', '700:B?order']);
  assert.deepEqual(show(spreadFamily([f('H-Bold', 400), f('H-BoldCondensed', 400), f('H-Regular', 400)], W)), ['400:H-Regular?name', '700:H-Bold?name', '800:H-BoldCondensed?order']);
  // Two same-weight pairs stay in weight order: a heavier request never gets a lighter file.
  assert.deepEqual(show(spreadFamily([f('Example-A', 300, 'Example'), f('Example-B', 300, 'Example'), f('Example-C', 500, 'Example'), f('Example-D', 500, 'Example')], W)), ['300:Example-A?order', '400:Example-B?order', '500:Example-C?order', '600:Example-D?order']);
});
