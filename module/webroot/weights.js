export function changeWeightMode(slot, mode, defaults) {
  if (slot.familyMode === mode) return;
  if (mode === 'continuous') {
    if (slot.weightMap) slot.savedWeightMap = {...slot.weightMap};
    delete slot.weightMap;
  } else {
    slot.weightMap = slot.savedWeightMap ? {...slot.savedWeightMap} : defaults(slot);
    delete slot.savedWeightMap;
  }
  slot.familyMode = mode;
}

// Shift every step of a nine-step map by delta, clamped to the font's wght axis.
export function shiftWeightMap(map, delta, axis) {
  const out = {};
  for (const [weight, value] of Object.entries(map)) {
    if (!Number.isFinite(value)) { out[weight] = value; continue; }
    let next = Math.round((value + delta) * 1000) / 1000;
    if (axis) next = Math.max(axis.min, Math.min(axis.max, next));
    out[weight] = next;
  }
  return out;
}

// Same rule as the engine: inside the axis, never below the last valid step.
// Returns a reason per weight, or '' when the step is fine.
export function weightMapProblems(map, weights, axis) {
  const problems = Object.fromEntries(weights.map(w => [w, '']));
  let prev = -Infinity, prevWeight = null;
  for (const w of weights) {
    const n = map[w];
    if (!Number.isFinite(n)) { problems[w] = '未填写'; continue; }
    if (axis && (n < axis.min || n > axis.max)) { problems[w] = `${n} 超出字体范围 ${axis.min}–${axis.max}`; continue; }
    if (n < prev) {
      problems[w] = `${n} 小于 ${prevWeight} 档的 ${prev}`;
      if (!problems[prevWeight]) problems[prevWeight] = `${prev} 大于 ${w} 档的 ${n}`;
      continue;
    }
    prev = n; prevWeight = w;
  }
  return problems;
}
