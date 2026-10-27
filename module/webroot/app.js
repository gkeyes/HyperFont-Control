import { changeWeightMode, shiftWeightMap, weightMapProblems } from './weights.js';
import { faceFamily, faceOptionLabel, fileStem, fontFamily, groupFonts, naturalCompare, spreadFamily } from './library.js';
const $ = (q, root = document) => root.querySelector(q);
const slots = [
  ['chinese', '中文', '汉', '汉字与全角标点', ''],
  ['latinSans', '无衬线英文', 'Aa', '常见界面中的英文与数字', ''],
  ['serif', '衬线英文', 'Ag', '阅读界面请求的衬线字体', 'serif'],
  ['monospace', '等宽英文', '01', '终端与代码中的等宽字体', 'mono'],
];
const weights = Array.from({ length: 9 }, (_, i) => (i + 1) * 100);
const pages = {
  settings: ['字体', () => '选择喜欢的字，保留自己的习惯。'],
  library: ['字体库', () => `${fonts.length} 个文件 · TTF、OTF、TTC/OTC，可变与静态自动识别`],
  diagnostics: ['诊断与恢复', () => '系统配置已覆盖，不等于每个应用都完成了显示验证。'],
};
const bootLabels = { mounted: '已挂载', system: '沿用系统字体', failed: '挂载未完成', guarded: '启动保护已暂停此配置', 'not-run-this-boot': '本次开机未运行' };
const isPreview = location.hostname === '127.0.0.1' && new URLSearchParams(location.search).get('preview') === '1';
const native = typeof window.ksu?.exec === 'function';
let serial = 0, busy = false, status, dirty = false, toastTimer;
let profile = { schemaVersion: 1, name: '我的字体组合', activation: 'manual-reboot', slots: Object.fromEntries(slots.map(([key]) => [key, { mode: 'system' }])) };
let fonts = [];
let selectedSlot = null, editorStep = 'font', libraryPage = 1, shiftAmount = 50, shiftBases = {}, modeMemory = {};
const openGroups = new Set(), downloadChecked = new Set();
let downloadFiles = [], downloadsLoaded = false;
const libraryPageSize = 6;


function text(tag, value, cls) { const e = document.createElement(tag); e.textContent = value; if (cls) e.className = cls; return e; }
function base64(bytes) { let out = ''; for (let i = 0; i < bytes.length; i += 8192) out += String.fromCharCode(...bytes.subarray(i, i + 8192)); return btoa(out); }
async function request(payload) {
  let reply;
  if (native) {
    // All user values travel inside base64 JSON. No label, path or font name is
    // interpolated as shell syntax. The executable and operation entry are fixed.
    const encoded = base64(new TextEncoder().encode(JSON.stringify(payload)));
    if (encoded.length >= 120000) throw new Error('单次请求过大');
    const result = await new Promise((resolve, reject) => {
      const callback = `hf_reply_${Date.now()}_${++serial}`;
      const timer = setTimeout(() => { delete window[callback]; reject(new Error('操作超时，请刷新状态后确认结果，避免重复提交。')); }, 180000);
      window[callback] = (errno, stdout, stderr) => { clearTimeout(timer); delete window[callback]; resolve({ errno, stdout, stderr }); };
      try { window.ksu.exec(`/data/adb/modules/hyperfont_control/bin/hfctl request '${encoded}'`, '{}', callback); }
      catch (e) { clearTimeout(timer); delete window[callback]; reject(e); }
    });
    try { reply = JSON.parse(result.stdout); } catch { throw new Error(`无法读取模块响应（${result.errno}）。请检查模块安装状态。`); }
  } else if (isPreview) {
    const res = await fetch('/api/request', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    reply = await res.json();
  } else throw new Error('请从支持 KernelSU WebUI 的模块管理器打开');
  if (!reply.ok) throw new Error(reply.error || '操作失败');
  return reply.data;
}
function markDirty() { dirty = true; $('#validation').hidden = true; }
function toast(message) { clearTimeout(toastTimer); $('#toast').textContent = message; $('#toast').hidden = false; toastTimer = setTimeout(() => $('#toast').hidden = true, 5000); }
function humanMessage(message) { return message.replace(/chinese:/g, '中文：').replace(/latinSans:/g, '无衬线英文：').replace(/serif:/g, '衬线英文：').replace(/monospace:/g, '等宽英文：').replace(/OEM/g, '厂商').replace(/全局 Hook/g, '全局应用适配'); }
function resultBox(box, message, items = [], error = false) { box.replaceChildren(); box.classList.toggle('error', error); if (items?.length) { const details = document.createElement('details'); details.append(text('summary', humanMessage(message) + ' · 查看说明')); const list = document.createElement('ul'); items.forEach(w => list.append(text('li', humanMessage(w)))); details.append(list); box.append(details); } else box.append(text('p', humanMessage(message))); box.hidden = false; box.scrollIntoView({ block: 'center', behavior: 'smooth' }); }
function validation(message, warnings = [], error = false) { resultBox($('#validation'), message, warnings, error); }
function renderImportErrors(message, items) { resultBox($('#import-errors'), message, items, true); $('#import-errors details').open = true; }
async function run(fn) { if (busy) return; busy = true; document.querySelectorAll('button, input, select').forEach(e => e.disabled = true); try { await fn(); } catch (e) { toast(e.message); validation(e.message, [], true); } finally { busy = false; document.querySelectorAll('button, input, select').forEach(e => e.disabled = !(native || isPreview)); updateDownloadButton(); } }
function confirmAction(title, message) { return new Promise(resolve => { const dialog = $('#confirm-dialog'); $('#dialog-title').textContent = title; $('#dialog-text').textContent = message; const finish = result => { dialog.close(); resolve(result); }; $('#dialog-ok').disabled = false; $('#dialog-cancel').disabled = false; $('#dialog-ok').onclick = () => finish(true); $('#dialog-cancel').onclick = () => finish(false); dialog.oncancel = e => { e.preventDefault(); finish(false); }; dialog.showModal(); }); }
function faceList(key, mode, italic = false) { return fonts.flatMap(a => a.faces.filter(f => (key === 'chinese' ? f.chinese : f.latin) && f.italic === italic && (mode === 'variable' ? f.axes.length > 0 && f.axes.some(x => x.tag === 'wght') : f.axes.length === 0)).map(f => ({ asset: a, face: f, value: `${a.id}:${f.index}` }))).sort((x, y) => naturalCompare(faceFamily(x.face) || fontFamily(x.asset), faceFamily(y.face) || fontFamily(y.asset)) || naturalCompare(x.asset.label, y.asset.label) || x.face.index - y.face.index); }
// Picker options grouped by font family, labelled by the file each face came from.
function faceOptions(select, list) { const groups = new Map(); for (const o of list) { const family = faceFamily(o.face) || fontFamily(o.asset); if (!groups.has(family)) groups.set(family, []); groups.get(family).push(o); } for (const [family, items] of groups) { const parent = items.length > 1 ? Object.assign(document.createElement('optgroup'), { label: `${family} · ${items.length}` }) : select; const sameWeight = new Set(items.map(o => o.face.weight)).size === 1 && items.length > 1; items.forEach(o => parent.append(option(o.value, faceOptionLabel(o.asset, o.face, !sameWeight)))); if (parent !== select) select.append(parent); } }
function faceMeta(id, index = 0) { return fonts.find(f => f.id === id)?.faces.find(f => f.index === index); }
function option(value, label) { const e = text('option', label); e.value = value; return e; }
function selectField(label, options, value, change, faces = []) { const wrap = text('label', '', 'field'); wrap.append(text('span', label)); const select = document.createElement('select'); select.setAttribute('aria-label', label); options.forEach(([v, l]) => select.append(option(v, l))); faceOptions(select, faces); select.value = value; select.addEventListener('change', () => { change(select.value); markDirty(); }); wrap.append(select); return wrap; }
// Switching font type brings back what this slot last had in that type: this session's edits first, then the pending or active configuration.
function rememberedSlot(key, mode) {
  if (mode === 'system') return null;
  const found = [modeMemory[key]?.[mode], status?.pending?.profile?.slots?.[key], status?.active?.profile?.slots?.[key]].find(x => x?.mode === mode);
  if (!found) return null;
  const slot = structuredClone(found); const has = (id, index) => !!faceMeta(id, index);
  if (mode === 'variable') { if (!has(slot.assetId, slot.faceIndex)) { slot.assetId = ''; slot.faceIndex = 0; } if (slot.italicAssetId && !has(slot.italicAssetId, slot.italicFaceIndex)) { delete slot.italicAssetId; delete slot.italicFaceIndex; } }
  else slot.faces = (slot.faces || []).filter(r => has(r.assetId, r.faceIndex));
  return slot;
}
function defaultMap(slot) { const a = faceMeta(slot.assetId, slot.faceIndex)?.axes.find(a => a.tag === 'wght'); return Object.fromEntries(weights.map(w => [w, a ? Math.max(a.min, Math.min(a.max, w)) : w])); }
function setRef(slot, value, italic = false) { const [id, index] = value.split(':'); if (italic) { if (id) { slot.italicAssetId = id; slot.italicFaceIndex = Number(index); } else { delete slot.italicAssetId; delete slot.italicFaceIndex; } } else { slot.assetId = id; slot.faceIndex = Number(index || 0); slot.axes = {}; delete slot.savedWeightMap; if (slot.familyMode === 'instances') slot.weightMap = defaultMap(slot); } }
function slotSummary(s) { if (s.mode === 'system') return '沿用系统字体'; if (s.mode === 'variable') return faceMeta(s.assetId, s.faceIndex)?.name || '选择一个可变字体'; return s.faces.length ? `静态多字重 · ${s.faces.length} 个字重面` : '选择静态字重文件'; }
function icon(d) { const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); svg.setAttribute('viewBox', '0 0 24 24'); svg.setAttribute('aria-hidden', 'true'); const path = document.createElementNS('http://www.w3.org/2000/svg', 'path'); path.setAttribute('d', d); svg.append(path); return svg; }
function segmentedField(label, options, value, change) { const wrap = text('div', '', 'field'); wrap.append(text('span', label)); const group = text('div', '', 'segmented'); group.setAttribute('role', 'group'); group.setAttribute('aria-label', label); for (const [v, l] of options) { const b = text('button', l); b.type = 'button'; b.setAttribute('aria-pressed', String(v === value)); b.onclick = () => { if (v === value) return; markDirty(); change(v); }; group.append(b); } wrap.append(group); return wrap; }
function pressed(id, on) { $(id).setAttribute('aria-pressed', String(on)); }
function showTab(tab) { if (tab === 'library' && !downloadsLoaded && $('#download-import').open && (native || isPreview)) setTimeout(() => run(loadDownloads)); if (tab !== document.body.dataset.tab) { $('#validation').hidden = true; $('#import-errors').hidden = true; } document.body.dataset.tab = tab; document.querySelectorAll('.tab-bar button').forEach(b => b.dataset.tab === tab ? b.setAttribute('aria-current', 'page') : b.removeAttribute('aria-current')); document.querySelectorAll('.tab-panel').forEach(p => p.hidden = p.id !== tab); renderTitle(); }
function renderTitle() { const [title, subtitle] = pages[document.body.dataset.tab]; $('#page-title').textContent = title; $('#page-subtitle').textContent = subtitle(); }
function weightBar(value, axis) { const lo = axis ? axis.min : 0, hi = axis ? axis.max : 1000; return `${Math.max(0, Math.min(100, (value - lo) / (hi - lo || 1) * 100))}%`; }
function renderSlots(openKey) {
  if (openKey !== undefined) selectedSlot = openKey;
  $('#settings-overview').hidden = selectedSlot !== null;
  $('#font-page').hidden = selectedSlot === null;
  $('#settings-footnote').hidden = selectedSlot !== null;
  document.body.classList.toggle('editing-font', selectedSlot !== null);
  const root = $('#slots'); root.replaceChildren();
  if (selectedSlot === null) {
    for (const [key, title, symbol, , glyphClass] of slots) {
      const s = profile.slots[key];
      const tile = text('button', '', `slot-tile glass${s.mode === 'system' ? ' system' : ''}`); tile.type = 'button'; tile.dataset.slot = key;
      tile.append(text('span', symbol, `slot-glyph ${glyphClass}`), text('strong', title), text('small', slotSummary(s)));
      tile.onclick = () => { editorStep = 'font'; renderSlots(key); window.scrollTo(0, 0); };
      root.append(tile);
    }
    return;
  }
  const index = slots.findIndex(([key]) => key === selectedSlot);
  const [key, title, , description] = slots[index];
  const s = profile.slots[key];
  $('#font-page-title').textContent = title;
  $('#font-page-position').textContent = `${index + 1} / ${slots.length} 个分类 · ${description}`;
  $('#slot-previous').classList.toggle('off', index === 0); $('#slot-next').classList.toggle('off', index === slots.length - 1);
  pressed('#step-font', editorStep === 'font'); pressed('#step-weight', editorStep === 'weight');
  const editor = text('section', '', 'slot-editor glass'); editor.dataset.slot = key;
  if (editorStep === 'font') {
    editor.append(segmentedField('字体类型', [['system', '沿用系统'], ['variable', '可变字体'], ['static', '静态多字重']], s.mode, mode => {
      (modeMemory[key] ??= {})[s.mode] = structuredClone(s);
      profile.slots[key] = rememberedSlot(key, mode) || (mode === 'system' ? { mode } : mode === 'variable' ? { mode, assetId: '', faceIndex: 0, familyMode: 'instances', weightMap: Object.fromEntries(weights.map(w => [w, w])) } : { mode, missingWeightPolicy: 'nearest', faces: [] }); renderSlots();
    }));
    if (s.mode === 'system') editor.append(text('p', '这一类沿用系统字体。选择“可变字体”或“静态多字重”后，可从字体库指定文件。', 'field-help'));
    if (s.mode === 'variable') {
      const options = faceList(key, 'variable');
      editor.append(selectField('正体文件', [['', options.length ? '请选择字体' : '请先在字体库导入可变字体']], s.assetId ? `${s.assetId}:${s.faceIndex || 0}` : '', value => { setRef(s, value); renderSlots(); }, options));
      const italics = faceList(key, 'variable', true);
      editor.append(selectField('斜体文件', [['', '未指定（由应用决定倾斜效果）']], s.italicAssetId ? `${s.italicAssetId}:${s.italicFaceIndex || 0}` : '', value => setRef(s, value, true), italics));
      editor.append(text('p', '选好文件后，切换到“② 字重设置”调整字重。', 'field-help'));
    }
    if (s.mode === 'static') {
      const entries = [...faceList(key, 'static'), ...faceList(key, 'static', true)];
      const familyOf = o => faceFamily(o.face) || fontFamily(o.asset); const families = [...new Set(entries.map(familyOf))].sort(naturalCompare);
      if (!entries.length) editor.append(text('p', '字体库里还没有可用的静态字体。到“字体库”从 Download 文件夹导入后再来选择。', 'field-help'));
      else {
        // One pick fills all nine steps from the family; step ② shows the result for fine-tuning.
        const first = s.faces.length && entries.find(o => o.asset.id === s.faces[0].assetId && o.face.index === s.faces[0].faceIndex); const current = first ? familyOf(first) : '';
        editor.append(selectField('字体家族', [['', '选择后自动填入九档'], ...families.map(f => [f, `${f} · ${entries.filter(o => familyOf(o) === f).length} 个字体面`])], families.includes(current) ? current : '', family => {
          if (!family) return;
          s.faces = spreadFamily(entries.filter(o => familyOf(o) === family), weights); editorStep = 'weight'; renderSlots();
          toast(`已用 ${family} 填入 ${new Set(s.faces.map(r => r.weight)).size} 档，可逐档调整`);
        }));
        editor.append(text('p', '选家族后按文件名从细到粗自动分配九档（文件自带不同字重时按字重），然后在“② 字重设置”逐档微调。', 'field-help'));
      }
    }
  } else {
    if (s.mode === 'system') editor.append(text('p', '当前沿用系统字体，没有需要修改的字重。可回到“① 选择字体”选择自己的字体。', 'field-help'));
    if (s.mode === 'variable') {
      const meta = faceMeta(s.assetId, s.faceIndex); const wght = meta?.axes.find(a => a.tag === 'wght');
      const heading = text('div', '', 'face-heading'); heading.append(text('strong', meta ? `${meta.name} · 可变` : '尚未选择字体'));
      if (wght) heading.append(text('span', `轴 ${wght.min}–${wght.max} · 默认 ${wght.default}`));
      editor.append(heading);
      editor.append(segmentedField('字重方式', [['instances', '自定义九档'], ['continuous', '按请求连续变化']], s.familyMode, mode => { changeWeightMode(s, mode, defaultMap); renderSlots(); }));
      if (s.familyMode === 'instances') {
        editor.append(text('p', '左侧是应用请求的字重，右侧填写该文件实际使用的轴值。', 'field-help'));
        const shift = text('div', '', 'weight-shift'); const amount = document.createElement('input'); amount.type = 'number'; amount.step = 'any'; amount.inputMode = 'decimal'; amount.value = shiftAmount; amount.oninput = () => shiftAmount = amount.value; amount.setAttribute('aria-label', '统一调整的数值');
        const applyShift = sign => {
          const delta = Number(amount.value); if (!Number.isFinite(delta) || delta === 0) { toast('请输入要统一调整的数值'); return; }
          // Shift from the values before the first shift, so capped steps get their spacing back when shifted the other way.
          let shiftBase = shiftBases[selectedSlot]; if (shiftBase?.last !== JSON.stringify(s.weightMap)) shiftBase = shiftBases[selectedSlot] = { map: {...s.weightMap}, offset: 0 };
          shiftBase.offset += sign * delta;
          const raw = shiftWeightMap(shiftBase.map, shiftBase.offset); s.weightMap = shiftWeightMap(shiftBase.map, shiftBase.offset, wght); shiftBase.last = JSON.stringify(s.weightMap);
          const capped = weights.some(w => raw[w] !== s.weightMap[w]);
          markDirty(); renderSlots(); toast(`九档已统一${sign > 0 ? '加' : '减'} ${delta}${capped ? `，超出 ${wght.min}–${wght.max} 的已封顶` : ''}`);
        };
        const minus = text('button', '整体减', 'glass-button'); minus.type = 'button'; minus.onclick = () => applyShift(-1);
        const plus = text('button', '整体加', 'glass-button'); plus.type = 'button'; plus.onclick = () => applyShift(1);
        shift.append(text('span', '统一调整'), minus, amount, plus); editor.append(shift);
        const rows = text('div', '', 'weight-rows'); const checks = []; const problemHint = text('p', '', 'field-help weight-problem');
        const markRows = () => { const problems = weightMapProblems(s.weightMap, weights, wght); for (const [w, row] of checks) { row.classList.toggle('invalid', !!problems[w]); row.title = problems[w]; } const first = weights.find(w => problems[w]); problemHint.textContent = first ? `${first} 档：${problems[first]}` : ''; problemHint.hidden = !first; };
        for (const w of weights) {
          const row = text('label', '', `weight-row${w === 400 ? ' regular' : ''}`);
          // Slider moves in steps of 25; the number box still takes any exact value.
          const slider = document.createElement('input'); slider.type = 'range'; slider.className = 'weight-slider'; slider.step = 25; slider.setAttribute('aria-label', `${title}请求 ${w} 的轴值滑块`);
          const lo = wght ? Math.ceil(wght.min / 25) * 25 : 0, hi = wght ? Math.floor(wght.max / 25) * 25 : 1000; slider.min = Math.min(lo, hi); slider.max = Math.max(lo, hi);
          const input = document.createElement('input'); input.type = 'number'; input.step = 'any'; input.inputMode = 'decimal'; input.value = s.weightMap[w]; input.setAttribute('aria-label', `${title}请求 ${w} 的实际轴值`); if (wght) { input.min = wght.min; input.max = wght.max; }
          const syncSlider = () => { slider.value = Number.isFinite(s.weightMap[w]) ? s.weightMap[w] : slider.min; slider.style.setProperty('--fill', weightBar(Number(slider.value), wght)); };
          syncSlider();
          input.oninput = () => { s.weightMap[w] = input.value === '' ? NaN : Number(input.value); syncSlider(); markDirty(); markRows(); };
          slider.oninput = () => { s.weightMap[w] = Number(slider.value); input.value = slider.value; slider.style.setProperty('--fill', weightBar(Number(slider.value), wght)); markDirty(); markRows(); };
          row.append(text('span', w === 400 ? '400 常规' : String(w)), slider, input); rows.append(row); checks.push([w, row]);
        }
        markRows(); editor.append(rows, problemHint);
      } else editor.append(text('p', '按应用请求选取字重，例如常规 400 使用文件轴 400。要自行加粗或减细，请选择“自定义九档”。原来的九档设置会保留，切回后恢复；保存并提交后需手动重启。', 'field-help'));
      const axes = meta?.axes.filter(a => a.tag !== 'wght') || [];
      if (axes.length) {
        const advanced = document.createElement('details'); advanced.className = 'advanced-axes'; advanced.append(text('summary', '其他可变轴'));
        for (const axis of axes) { const label = text('label', '', 'field'); label.append(text('span', `${axis.tag} · ${axis.min}–${axis.max}`)); const input = document.createElement('input'); input.type = 'number'; input.step = 'any'; input.min = axis.min; input.max = axis.max; input.value = s.axes?.[axis.tag] ?? axis.default; input.oninput = () => { s.axes ||= {}; s.axes[axis.tag] = input.value === '' ? NaN : Number(input.value); markDirty(); }; label.append(input); advanced.append(label); } editor.append(advanced);
      }
    }
    if (s.mode === 'static') {
      editor.append(selectField('缺少字重时', [['nearest', '使用最近的已有字重'], ['reject', '必须提供完整九档正体']], s.missingWeightPolicy, value => s.missingWeightPolicy = value));
      const table = document.createElement('table'); table.className = 'weight-table'; const head = document.createElement('thead'); const tr = document.createElement('tr'); ['请求', '正体文件', '斜体文件'].forEach(t => tr.append(text('th', t))); head.append(tr); table.append(head); const body = document.createElement('tbody');
      for (const weight of weights) { const row = document.createElement('tr'); row.append(text('td', weight)); for (const style of ['normal', 'italic']) { const cell = document.createElement('td'); const sel = document.createElement('select'); sel.setAttribute('aria-label', `${title} ${weight} ${style === 'normal' ? '正体' : '斜体'}`); sel.append(option('', '未指定')); faceOptions(sel, faceList(key, 'static', style === 'italic')); const ref = s.faces.find(r => r.weight === weight && r.style === style); sel.value = ref ? `${ref.assetId}:${ref.faceIndex}` : ''; sel.onchange = () => { s.faces = s.faces.filter(r => r.weight !== weight || r.style !== style); if (sel.value) { const [assetId, index] = sel.value.split(':'); s.faces.push({ assetId, faceIndex: Number(index), weight, style }); } markDirty(); }; cell.append(sel); row.append(cell); } body.append(row); } table.append(body); editor.append(table);
    }
  }
  root.append(editor);
}
function fontTags(font) {
  const tags = []; const variable = font.faces.flatMap(f => f.axes.filter(a => a.tag === 'wght'));
  if (variable.length) tags.push(`可变 wght ${Math.min(...variable.map(a => a.min))}–${Math.max(...variable.map(a => a.max))}`);
  const fixed = [...new Set(font.faces.filter(f => !f.axes.length).map(f => f.weight))].sort((a, b) => a - b);
  if (fixed.length) tags.push(`静态 ${fixed.length > 1 ? `${fixed[0]}–${fixed.at(-1)}` : fixed[0]}`);
  if (font.faces.some(f => f.italic)) tags.push('含斜体');
  if (font.faces.some(f => f.chinese)) tags.push('中文');
  if (font.faces.some(f => f.latin)) tags.push('英文');
  return tags;
}
// Where an asset is referenced. The engine refuses to delete fonts used by the saved draft,
// the pending or the active configuration; unsaved edits only need re-picking afterwards.
function fontUses(id, saved = true) {
  const refs = p => Object.values(p?.slots || {}).flatMap(x => [x.assetId, x.italicAssetId, ...(x.faces || []).map(r => r.assetId)]);
  const sources = saved ? [['已保存的草稿', status?.draft], ['待生效配置', status?.pending?.profile], ['正在使用的配置', status?.active?.profile]] : [['未保存的编辑', profile]];
  return sources.filter(([, p]) => refs(p).includes(id)).map(([name]) => name);
}
async function removeFonts(list) {
  const blocked = list.filter(f => fontUses(f.id).length), removable = list.filter(f => !fontUses(f.id).length);
  const why = [...new Set(blocked.flatMap(f => fontUses(f.id)))].join('、');
  if (!removable.length) { toast(`${list.length === 1 ? fileStem(list[0].label) : '这组文件'}正被${why}使用，先在“字体设置”换成别的字体并保存，再来移除`); return; }
  const what = removable.length === 1 ? `“${fileStem(removable[0].label)}”` : `${removable.length} 个文件`;
  const notes = [blocked.length ? `其中 ${blocked.length} 个正被${why}使用，会保留。` : '', removable.some(f => fontUses(f.id, false).length) ? '当前未保存的编辑用到了它，移除后需要重新选择。' : '', '移除后无法撤销，需要时请重新导入。'].join('');
  if (!await confirmAction(list.length === 1 ? '移除字体' : '移除整组', `从模块字体库移除${what}？${notes}`)) return;
  run(async () => {
    let removed = 0;
    try { for (const f of removable) { await request({ op: 'delete_font', id: f.id }); removed++; } }
    finally { await loadStatus(true); }
    toast(`已从字体库移除 ${removed} 个文件${blocked.length ? `，${blocked.length} 个仍在使用已保留` : ''}`);
  });
}
function fontEntry(font, compact) {
  const item = text('article', '', `font-entry${compact ? ' compact' : ''}`);
  const spec = text('span', font.faces.some(f => f.chinese) ? '永' : 'Aa', 'font-spec'); spec.setAttribute('aria-hidden', 'true');
  const copy = text('div'); copy.append(text('h3', fileStem(font.label)));
  // Inside a family group the header already carries the tags; keep each file to one line.
  if (compact) copy.append(text('p', `${(font.size / 1048576).toFixed(1)} MiB · ${fontTags(font)[0] || ''}`, 'font-meta'));
  else { copy.append(text('p', `${(font.size / 1048576).toFixed(1)} MiB · ${font.faces.length} 个字体面${font.faces.length === 1 ? ` · ${font.faces[0].name}` : ''}`, 'font-meta')); const tags = text('div', '', 'tags'); fontTags(font).forEach(t => tags.append(text('span', t, 'tag'))); copy.append(tags); }
  const del = text('button', '', 'remove-font'); del.type = 'button'; del.setAttribute('aria-label', `移除 ${font.label}`); del.append(icon('M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6'));
  del.onclick = () => { if (!busy) removeFonts([font]); };
  if (compact) item.append(copy, del); else item.append(spec, copy, del);
  if (font.faces.length > 1) { const faces = document.createElement('details'); faces.append(text('summary', `查看 ${font.faces.length} 个字体面`)); for (const f of font.faces) { const axes = f.axes.length ? `可变 ${f.axes.map(a => `${a.tag} ${a.min}–${a.max}`).join(' / ')}` : `静态 ${f.weight}`; faces.append(text('p', `${f.name} · ${axes}${f.italic ? ' · 斜体' : ''} · ${[f.chinese && '中文', f.latin && '英文'].filter(Boolean).join(' ')}`)); } item.append(faces); }
  return item;
}
function renderLibrary() {
  const root = $('#font-list'); root.replaceChildren(); $('#font-count').textContent = fonts.length; renderTitle();
  const groups = groupFonts(fonts);
  $('#library-pages').hidden = groups.length <= libraryPageSize;
  if (!fonts.length) { root.append(text('div', '还没有导入字体。\n点右上角“导入字体”或上方“从 Download 文件夹导入”添加文件，再回到“字体设置”选择它。', 'empty')); return; }
  const pageCount = Math.max(1, Math.ceil(groups.length / libraryPageSize)); libraryPage = Math.min(pageCount, libraryPage);
  $('#library-page-label').textContent = `${libraryPage} / ${pageCount} 页`; $('#library-prev').classList.toggle('off', libraryPage === 1); $('#library-next').classList.toggle('off', libraryPage === pageCount);
  for (const group of groups.slice((libraryPage - 1) * libraryPageSize, libraryPage * libraryPageSize)) {
    if (group.fonts.length === 1) { root.append(fontEntry(group.fonts[0], false)); continue; }
    // Files of one family collapse into a group; the open state survives re-renders.
    const box = document.createElement('details'); box.className = 'font-group'; box.open = openGroups.has(group.family); box.ontoggle = () => box.open ? openGroups.add(group.family) : openGroups.delete(group.family);
    const summary = document.createElement('summary'); const spec = text('span', group.fonts.some(f => f.faces.some(x => x.chinese)) ? '永' : 'Aa', 'font-spec'); spec.setAttribute('aria-hidden', 'true');
    const copy = text('div'); const size = group.fonts.reduce((n, f) => n + f.size, 0);
    copy.append(text('h3', group.family), text('p', `${group.fonts.length} 个文件 · ${(size / 1048576).toFixed(1)} MiB`, 'font-meta'));
    const tags = text('div', '', 'tags'); [...new Set(group.fonts.flatMap(fontTags))].forEach(t => tags.append(text('span', t, 'tag'))); copy.append(tags);
    summary.append(spec, copy, text('span', '', 'chevron')); box.append(summary);
    const list = text('div', '', 'group-files'); group.fonts.forEach(f => list.append(fontEntry(f, true)));
    const removeAll = text('button', `移除整组 ${group.fonts.length} 个文件`, 'glass-button quiet danger-text'); removeAll.type = 'button'; removeAll.onclick = () => { if (!busy) removeFonts(group.fonts); };
    list.append(removeAll); box.append(list); root.append(box);
  }
}
function renderStatus() {
  const active = status.active; const boot = status.boot; const pending = status.pending;
  $('#version').textContent = status.version; $('#status-dot').className = 'status-dot'; $('.status-title').classList.toggle('failed', ['failed', 'guarded'].includes(boot.state));
  if (boot.state === 'guarded') { $('#status-title').textContent = '已暂停这份字体配置'; $('#status-description').textContent = boot.error; $('#status-dot').classList.add('failed'); }
  else if (boot.state === 'failed') { $('#status-title').textContent = '本次挂载未完成'; $('#status-description').textContent = boot.error || '请查看诊断信息'; $('#status-dot').classList.add('failed'); }
  else if (status.visible) { $('#status-title').textContent = '字体覆盖已挂载'; $('#status-description').textContent = `${active?.profile.name || ''} · 已核对字体表和文件内容，界面效果仍需检查。`; }
  else if (boot.state === 'mounted') { $('#status-title').textContent = '覆盖状态待核对'; $('#status-description').textContent = status.visibilityError || '当前未读到完整的覆盖配置，请查看诊断或恢复系统字体后重试。'; $('#status-dot').classList.add('pending'); }
  else { $('#status-title').textContent = boot.state === 'system' ? '当前沿用系统字体' : '尚未应用字体配置'; $('#status-description').textContent = '选择字体并保存后，手动重启即可尝试新配置。'; }
  $('#pending-description').hidden = !pending;
  if (pending) { const blocked = boot.state === 'guarded' && pending.id === boot.generation; $('#pending-description').textContent = `${blocked ? '已暂停，需重新提交' : '待重启使用'}：${pending.profile.name}`; if (!['failed', 'guarded'].includes(boot.state)) $('#status-dot').classList.add('pending'); }
  const dl = $('#diagnostic-details'); dl.replaceChildren(); for (const [label, value] of [['核心版本', status.version], ['Android SDK', status.baseline?.sdk || '尚未识别'], ['挂载方式', '开机临时覆盖'], ['最近开机结果', (boot.state ? bootLabels[boot.state] || `未知状态（${boot.state}）` : '尚未运行')], ['验证层级', status.visible ? '字体表与文件内容校验' : '尚无当前挂载证据'], ['应用界面实测', '待用户重启后验收']]) dl.append(text('dt', label), text('dd', value));
  const compat = status.fontCompatibility;
  if (compat?.needed) dl.append(text('dt', '小米默认字重'), text('dd', compat.preparedThisBoot ? '兼容层已准备；需启用 Zygisk，应用效果待验收' : '请启用 Zygisk，并手动重启以加载兼容层'));
}
async function loadStatus(keepDraft = false) { $('#validation').hidden = true; status = await request({ op: 'status' }); fonts = status.library || []; if (!keepDraft) { profile = structuredClone(status.draft); modeMemory = {}; shiftBases = {}; dirty = false; $('#profile-name').value = profile.name; } renderStatus(); renderSlots(); renderLibrary(); }
function currentProfile() { profile.name = $('#profile-name').value.trim(); const encoded = JSON.stringify(profile); if (encoded.includes(':null')) throw new Error('请填写所有字重与轴值'); return profile; }
function moveSlot(step) { const index = slots.findIndex(([key]) => key === selectedSlot) + step; if (index < 0 || index >= slots.length) return; renderSlots(slots[index][0]); window.scrollTo(0, 0); }
function setStep(step) { editorStep = step; renderSlots(); window.scrollTo(0, 0); }
document.querySelectorAll('.tab-bar button').forEach(button => button.onclick = () => showTab(button.dataset.tab));
$('#profile-name').oninput = () => markDirty();
$('#back-overview').onclick = () => { renderSlots(null); window.scrollTo(0, 0); };
$('#slot-previous').onclick = () => moveSlot(-1);
$('#slot-next').onclick = () => moveSlot(1);
$('#step-font').onclick = () => setStep('font');
$('#step-weight').onclick = () => setStep('weight');
$('#library-next').onclick = () => { if ($('#library-next').classList.contains('off')) return; libraryPage++; renderLibrary(); window.scrollTo(0, 0); };
$('#library-prev').onclick = () => { if ($('#library-prev').classList.contains('off')) return; libraryPage--; renderLibrary(); window.scrollTo(0, 0); };
for (const key of ['coverage', 'runtime']) $('#show-' + key).onclick = () => { $('#coverage-page').hidden = key !== 'coverage'; $('#runtime-page').hidden = key !== 'runtime'; pressed('#show-coverage', key === 'coverage'); pressed('#show-runtime', key === 'runtime'); };
$('#refresh').onclick = () => run(async () => { if (!dirty || await confirmAction('重新读取配置', '这会放弃尚未保存的编辑。')) await loadStatus(); });
$('#validate').onclick = () => run(async () => { const r = await request({ op: 'validate', profile: currentProfile() }); validation('配置检查通过', r.warnings); });
$('#save').onclick = () => run(async () => { const saved = currentProfile(); const r = await request({ op: 'save', profile: saved }); status.draft = structuredClone(saved); dirty = false; validation('草稿已保存，尚未提交生效', r.warnings); toast('草稿已保存'); });
$('#stage').onclick = () => run(async () => { const r = await request({ op: 'stage', profile: currentProfile() }); dirty = false; selectedSlot = null; await loadStatus(); validation('已保存待生效配置。请手动重启后检查通知、设置和常用应用。', r.warnings); toast('已保存，等待你手动重启'); });
$('#restore').onclick = async () => { if (await confirmAction('恢复系统字体', '将保存一份系统字体配置，手动重启后撤销本模块的覆盖。字体库和你的草稿仍保留。')) run(async () => { await request({ op: 'restore' }); await loadStatus(true); toast('已准备恢复，手动重启后生效'); }); };
$('#cancel-pending').onclick = () => run(async () => { await request({ op: 'cancel_pending' }); await loadStatus(true); toast('已取消待生效配置'); });
function downloadJSON(name, data) { const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })); const a = document.createElement('a'); a.href = url; a.download = name; a.click(); setTimeout(() => URL.revokeObjectURL(url), 10000); }
$('#export').onclick = () => run(async () => { if (native) { const r = await request({ op: 'export_diagnostics' }); toast(`已保存到 ${r.path}`); } else downloadJSON('HyperFont-diagnostics.json', await request({ op: 'diagnostics' })); });
$('#export-profile').onclick = () => run(async () => { const p = currentProfile(); await request({ op: 'validate', profile: p }); if (native) { const r = await request({ op: 'export_profile', profile: p }); toast(`已保存到 ${r.path}`); } else downloadJSON('HyperFont-profile.json', p); });
$('#profile-file').onchange = event => run(async () => { const file = event.target.files[0]; if (!file) return; try { if (file.size > 65536) throw new Error('配置文件过大'); const p = JSON.parse(await file.text()); if (dirty && !await confirmAction('导入配置', '导入会替换当前草稿，尚未保存的编辑将丢失。')) return; await request({ op: 'save', profile: p }); await loadStatus(); toast('配置已导入为草稿，请检查后提交'); } finally { $('#profile-file').value = ''; } });
$('#refresh-baseline').onclick = async () => { if (await confirmAction('重新识别系统', '适用于系统升级后。本模块仍在覆盖字体时，请先恢复系统字体并重启。')) run(async () => { await request({ op: 'refresh_baseline' }); await loadStatus(true); toast('已识别当前系统，请重新提交配置'); }); };
// Download folder: list font files in the engine and import the ticked ones by path, no WebView upload.
function renderDownloads() {
  const root = $('#download-list'); root.replaceChildren();
  if (!downloadFiles.length) root.append(text('div', native || isPreview ? 'Download 文件夹里没有 TTF、OTF、TTC、OTC 字体。把字体放进去后点“刷新列表”。' : '请从模块管理器打开此面板。', 'download-empty'));
  for (const f of downloadFiles) {
    const row = text('label', '', `download-row${f.imported ? ' done' : ''}`); const box = document.createElement('input'); box.type = 'checkbox'; box.checked = downloadChecked.has(f.path);
    box.onchange = () => { box.checked ? downloadChecked.add(f.path) : downloadChecked.delete(f.path); updateDownloadButton(); };
    const copy = text('span');
    copy.append(text('strong', f.name), text('small', `${(f.size / 1048576).toFixed(1)} MiB${f.folder ? ` · ${f.folder}` : ''}`));
    row.append(box, copy); if (f.imported) row.append(text('span', '已导入', 'tag')); root.append(row);
  }
  updateDownloadButton();
}
function updateDownloadButton() { const n = downloadChecked.size; $('#download-import-button').disabled = busy || !n || !(native || isPreview); $('#download-import-button').textContent = n ? `导入所选 ${n} 个` : '导入所选'; }
async function loadDownloads() { downloadFiles = await request({ op: 'list_downloads' }); downloadsLoaded = true; const paths = new Set(downloadFiles.map(f => f.path)); for (const p of [...downloadChecked]) if (!paths.has(p)) downloadChecked.delete(p); renderDownloads(); }
$('#download-import').ontoggle = () => { if ($('#download-import').open && (native || isPreview)) run(loadDownloads); };
$('#download-refresh').onclick = () => run(loadDownloads);
// The top import button goes straight to the Download list: fastest path, no WebView upload.
$('#import-open').onclick = () => { showTab('library'); const panel = $('#download-import'); if (panel.open) run(loadDownloads); else panel.open = true; window.scrollTo(0, 0); };
$('#download-all').onclick = () => { const fresh = downloadFiles.filter(f => !f.imported); const all = fresh.length && fresh.every(f => downloadChecked.has(f.path)); fresh.forEach(f => all ? downloadChecked.delete(f.path) : downloadChecked.add(f.path)); renderDownloads(); };
$('#download-import-button').onclick = () => run(async () => {
  $('#import-errors').hidden = true; const picked = downloadFiles.filter(f => downloadChecked.has(f.path)); const failed = []; let imported = 0; $('#import-progress').hidden = false;
  try {
    for (const [i, f] of picked.entries()) {
      $('#import-message').textContent = `${f.name} · ${i + 1} / ${picked.length}`; $('#import-meter').value = Math.round(i / picked.length * 100);
      try { await request({ op: 'import_path', path: f.path }); imported++; downloadChecked.delete(f.path); } catch (e) { failed.push(`${f.name}：${e.message}`); }
    }
    if (imported) await loadStatus(true);
    await loadDownloads();
  } finally { $('#import-progress').hidden = true; }
  const summary = failed.length ? `已导入 ${imported} 个，${failed.length} 个失败` : `已导入 ${imported} 个字体文件`;
  toast(summary); if (failed.length) renderImportErrors(summary, failed);
});
$('#cleanup-uploads').onclick = async () => { if (busy) return; if (await confirmAction('清理未完成导入', '清理未完成上传的临时文件。其他面板中正在进行的导入也会取消；已导入的字体和当前设置保留。')) run(async () => { const r = await request({ op: 'cleanup_uploads' }); toast(`已清理 ${r.removed} 项未完成导入`); }); };
$('#font-file').onchange = event => run(async () => {
  $('#import-errors').hidden = true; const files = [...event.target.files]; const failed = []; let imported = 0; $('#import-progress').hidden = false;
  try {
    for (const file of files) {
      let upload;
      try {
        if (file.size > 256 * 1048576) throw new Error('单个字体不能超过 256 MiB');
        upload = await request({ op: 'upload_begin', size: file.size, label: file.name });
        for (let offset = 0; offset < file.size; offset += 48 * 1024) { const chunk = new Uint8Array(await file.slice(offset, offset + 48 * 1024).arrayBuffer()); await request({ op: 'upload_chunk', token: upload.token, offset, chunk: base64(chunk) }); $('#import-meter').value = Math.round((offset + chunk.length) / file.size * 100); $('#import-message').textContent = `${file.name} · ${$('#import-meter').value}%`; }
        await request({ op: 'upload_finish', token: upload.token }); imported++;
      } catch (e) {
        if (upload) { try { await request({ op: 'upload_cancel', token: upload.token }); } catch { /* A later import can retry; never hide original failure. */ } }
        // Keep going: one bad file should not stop the rest of the batch.
        failed.push(`${file.name}：${e.message}`);
      }
    }
    if (imported) await loadStatus(true);
  } finally { $('#font-file').value = ''; $('#import-progress').hidden = true; }
  if (!failed.length) { toast(`已导入 ${imported} 个字体文件`); return; }
  const summary = `已导入 ${imported} 个，${failed.length} 个失败`;
  toast(summary);
  if (document.body.dataset.tab === 'library') renderImportErrors(summary, failed); else validation(summary, failed, true);
});
$('#preview-banner').hidden = !isPreview; renderTitle();
if (!native && !isPreview) { $('#offline').hidden = false; $('#status-title').textContent = '尚未连接模块'; $('#status-description').textContent = '从模块管理器进入 WebUI 后可读取手机配置。'; renderSlots(); renderLibrary(); document.querySelectorAll('button,input,select').forEach(e => e.disabled = true); document.querySelectorAll('.tab-bar button').forEach(e => e.disabled = false); }
else run(() => loadStatus());
