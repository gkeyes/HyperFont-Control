// Pure helpers for naming and grouping library fonts; no DOM access.

const styleWord = /^(regular|normal|book|roman|plain|italic|oblique|thin|hairline|light|medium|bold|heavy|black|(extra|ultra|semi|demi)-?(light|bold|black)?|\d{2,3}w?)$/i;

export const naturalCompare = (a, b) => String(a).localeCompare(String(b), undefined, { numeric: true, sensitivity: 'base' });

// Import label without the font extension, e.g. "HYQiHeiClassic-25S".
export function fileStem(label) { return String(label || '').replace(/\.(ttf|otf|ttc|otc)$/i, ''); }

// Family name of a face: its name with trailing style words removed.
export function faceFamily(face) {
  const words = String(face?.name || '').trim().split(/\s+/).filter(Boolean);
  while (words.length > 1 && styleWord.test(words[words.length - 1])) words.pop();
  return words.join(' ');
}

// Family of a library asset; falls back to the file name when the font has no name.
export function fontFamily(font) { return faceFamily(font.faces?.[0]) || fileStem(font.label); }

// Groups assets by family. Groups and the files inside them are in natural order of file name.
export function groupFonts(fonts) {
  const groups = new Map();
  for (const font of fonts) {
    const family = fontFamily(font); const key = family.toLowerCase();
    if (!groups.has(key)) groups.set(key, { family, fonts: [] });
    groups.get(key).fonts.push(font);
  }
  const out = [...groups.values()];
  for (const g of out) g.fonts.sort((a, b) => naturalCompare(a.label, b.label));
  return out.sort((a, b) => naturalCompare(a.family, b.family));
}

// What to show for one face in a picker: the file it came from, plus the face name
// when the file holds several faces, plus the weight the file declares.
export function faceOptionLabel(font, face, withWeight = true) {
  const parts = [fileStem(font.label)];
  if (font.faces.length > 1) parts.push(face.name);
  if (withWeight && !face.axes?.length) parts.push(String(face.weight));
  return parts.join(' · ');
}

// Fill the nine weight steps from a family's static files. Files that declare
// distinct weights go to their own steps; otherwise (many families declare 400 in
// every file and encode the weight in the name) they are spread thinnest first
// in file-name order.
// Returns face refs for each style; steps without a file are left out.
export function spreadFamily(entries, weights) {
  const out = [];
  for (const style of ['normal', 'italic']) {
    const list = entries.filter(e => !!e.face.italic === (style === 'italic'));
    if (!list.length) continue;
    const distinct = new Set(list.map(e => e.face.weight)).size === list.length;
    list.sort((a, b) => (distinct ? a.face.weight - b.face.weight : 0) || naturalCompare(a.asset.label, b.asset.label) || a.face.index - b.face.index);
    const n = list.length, last = weights.length - 1;
    const pick = (e, weight) => out.push({ assetId: e.asset.id, faceIndex: e.face.index, weight, style });
    // Files that declare their own distinct weights go to those steps.
    const declared = list.map(e => Math.min(weights[last], Math.max(weights[0], Math.round(e.face.weight / 100) * 100)));
    if (distinct && new Set(declared).size === n && declared.every(w => weights.includes(w))) list.forEach((e, j) => pick(e, declared[j]));
    else if (n >= weights.length) weights.forEach((w, i) => pick(list[Math.round(i * (n - 1) / last)], w));
    else if (n === 1) pick(list[0], 400);
    else list.forEach((e, j) => pick(e, weights[Math.round(j * last / (n - 1))]));
  }
  return out;
}
