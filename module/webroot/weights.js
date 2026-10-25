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
