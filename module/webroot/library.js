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
// no other file shares goes to that step, and so does one whose weight read from its
// file or face name is unique. Files that still share a weight (many families declare
// 400 in every file) take the free steps nearest that weight, in file-name order; when
// there are more of them than free steps they are spread over all steps thinnest first.
// Returns face refs for each style; a ref not set from its declared weight carries
// guess: 'name' (weight read from the name) or 'order' (placed by file-name order).
export function spreadFamily(entries, weights) {
  const out = [];
  const step = w => Math.min(weights[weights.length - 1], Math.max(weights[0], Math.round(w / 100) * 100));
  const tally = list => list.reduce((m, w) => m.set(w, (m.get(w) || 0) + 1), new Map());
  const byName = (a, b) => naturalCompare(a.asset.label, b.asset.label) || a.face.index - b.face.index;
  for (const style of ['normal', 'italic']) {
    const list = entries.filter(e => !!e.face.italic === (style === 'italic'));
    if (!list.length) continue;
    const taken = new Map(), pick = (e, weight, guess) => { taken.set(weight, true); out.push({ assetId: e.asset.id, faceIndex: e.face.index, weight, style, ...(guess ? { guess } : {}) }); };
    const declared = tally(list.map(e => step(e.face.weight)));
    let rest = list.filter(e => { const w = step(e.face.weight); if (declared.get(w) === 1 && weights.includes(w)) { pick(e, w); return false; } return true; });
    const named = e => nameWeight(fileStem(e.asset.label)) ?? nameWeight(e.face.name);
    const fromName = tally(rest.map(named).filter(Boolean));
    rest = rest.filter(e => { const w = named(e); if (w && fromName.get(w) === 1 && weights.includes(w) && !taken.has(w)) { pick(e, w, 'name'); return false; } return true; });
    if (!rest.length) continue;
    let free = weights.filter(w => !taken.has(w));
    if (rest.length >= free.length) {
      rest.sort((a, b) => a.face.weight - b.face.weight || byName(a, b));
      const n = rest.length, last = free.length - 1;
      free.forEach((w, i) => pick(rest[last ? Math.round(i * (n - 1) / last) : Math.floor((n - 1) / 2)], w, 'order'));
      continue;
    }
    // Files that share a weight take a run of free steps nearest it, the run that starts
    // at that weight winning a tie. Two files with nothing in their names to tell them
    // apart, and no other files to place, are most often a regular and a bold, so they go
    // to that weight and +300. The steps found are then handed out in weight order, so a
    // heavier request never gets a lighter file.
    const target = e => named(e) || step(e.face.weight), groups = new Map(), steps = [];
    for (const e of rest.sort(byName)) { const t = target(e); if (!groups.has(t)) groups.set(t, []); groups.get(t).push(e); }
    for (const [t, group] of [...groups].sort(([a], [b]) => a - b)) {
      free = weights.filter(w => !taken.has(w) && !steps.includes(w));
      if (groups.size === 1 && group.length === 2 && !group.some(named) && free.includes(t)) {
        steps.push(t, free.filter(w => w !== t).sort((a, b) => Math.abs(a - t - 300) - Math.abs(b - t - 300))[0]); continue;
      }
      let best = 0, cost = Infinity;
      for (let i = 0; i + group.length <= free.length; i++) {
        const c = free.slice(i, i + group.length).reduce((sum, w) => sum + Math.abs(w - t), 0);
        if (c < cost || (c === cost && free[i] === t)) { best = i; cost = c; }
      }
      steps.push(...free.slice(best, best + group.length));
    }
    steps.sort((a, b) => a - b);
    rest.sort((a, b) => target(a) - target(b) || a.face.weight - b.face.weight || byName(a, b)).forEach((e, j) => pick(e, steps[j], named(e) === steps[j] ? 'name' : 'order'));
  }
  return out.sort((a, b) => (a.style === 'italic') - (b.style === 'italic') || a.weight - b.weight);
}

// Groups Download files by folder (top level first), each in natural order of name.
export function groupDownloads(files) {
  const groups = new Map();
  for (const f of files) { const key = f.folder || ''; if (!groups.has(key)) groups.set(key, []); groups.get(key).push(f); }
  return [...groups].sort(([a], [b]) => (a === '') !== (b === '') ? (a === '' ? -1 : 1) : naturalCompare(a, b)).map(([folder, list]) => ({ folder, files: list.sort((a, b) => naturalCompare(a.name, b.name)) }));
}
