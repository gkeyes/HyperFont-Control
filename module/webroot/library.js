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

const weightWords = { thin: 100, hairline: 100, extralight: 200, ultralight: 200, light: 300, regular: 400, normal: 400, book: 400, roman: 400, medium: 500, semibold: 600, demibold: 600, demi: 600, bold: 700, extrabold: 800, ultrabold: 800, heavy: 800, black: 900, extrablack: 900, ultrablack: 900 };

// Weight written in a name, e.g. "Noto-SemiBold", "Hiragino W6" or "Font_300".
// The last match wins, since the style usually comes last; null when there is none.
export function nameWeight(name) {
  const words = String(name || '').replace(/([a-z])([A-Z])/g, '$1 $2').toLowerCase().split(/[^a-z0-9]+/).filter(Boolean);
  let found = null;
  for (let i = 0; i < words.length; i++) {
    const pair = weightWords[words[i] + (words[i + 1] || '')];
    if (pair && i + 1 < words.length) { found = pair; i++; continue; }
    const w = words[i];
    if (weightWords[w]) found = weightWords[w];
    else if (/^[1-9]00$/.test(w)) found = Number(w);
    else if (/^w[1-9]$/.test(w)) found = Number(w[1]) * 100;
  }
  return found;
}

// Fill the nine weight steps from a family's static files. A file whose declared weight
// no other file shares goes to that step. Files that share one (many families declare
// 400 in every file) go to the weight in their file or face name when that is unique;
// the rest are spread over the free steps thinnest first in file-name order.
// Returns face refs for each style; refs not set from a declared weight carry guess: true.
export function spreadFamily(entries, weights) {
  const out = [];
  const step = w => Math.min(weights[weights.length - 1], Math.max(weights[0], Math.round(w / 100) * 100));
  for (const style of ['normal', 'italic']) {
    const list = entries.filter(e => !!e.face.italic === (style === 'italic'));
    if (!list.length) continue;
    const pick = (e, weight, guess) => out.push({ assetId: e.asset.id, faceIndex: e.face.index, weight, style, ...(guess ? { guess: true } : {}) });
    const taken = new Map(), count = (map, w) => map.set(w, (map.get(w) || 0) + 1), declared = new Map();
    list.forEach(e => count(declared, step(e.face.weight)));
    let rest = [];
    for (const e of list) { const w = step(e.face.weight); if (declared.get(w) === 1 && weights.includes(w)) taken.set(w, e); else rest.push(e); }
    const named = new Map(), guessOf = e => nameWeight(fileStem(e.asset.label)) ?? nameWeight(e.face.name);
    rest.forEach(e => { const w = guessOf(e); if (w) count(named, w); });
    rest = rest.filter(e => { const w = guessOf(e); if (w && named.get(w) === 1 && weights.includes(w) && !taken.has(w)) { taken.set(w, e); e.named = true; return false; } return true; });
    for (const [w, e] of taken) pick(e, w, e.named);
    list.forEach(e => delete e.named);
    rest.sort((a, b) => a.face.weight - b.face.weight || naturalCompare(a.asset.label, b.asset.label) || a.face.index - b.face.index);
    const free = weights.filter(w => !taken.has(w)), n = rest.length, last = free.length - 1;
    if (!n || !free.length) continue;
    if (n >= free.length) free.forEach((w, i) => pick(rest[last ? Math.round(i * (n - 1) / last) : Math.floor((n - 1) / 2)], w, true));
    else if (n === 1) pick(rest[0], free.includes(400) ? 400 : free[Math.floor(last / 2)], true);
    else rest.forEach((e, j) => pick(e, free[Math.round(j * last / (n - 1))], true));
  }
  return out.sort((a, b) => (a.style === 'italic') - (b.style === 'italic') || a.weight - b.weight);
}

// Groups Download files by folder (top level first), each in natural order of name.
export function groupDownloads(files) {
  const groups = new Map();
  for (const f of files) { const key = f.folder || ''; if (!groups.has(key)) groups.set(key, []); groups.get(key).push(f); }
  return [...groups].sort(([a], [b]) => (a === '') !== (b === '') ? (a === '' ? -1 : 1) : naturalCompare(a, b)).map(([folder, list]) => ({ folder, files: list.sort((a, b) => naturalCompare(a.name, b.name)) }));
}
