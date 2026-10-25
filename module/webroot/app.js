import { changeWeightMode } from './weights.js';
const $ = (q, root = document) => root.querySelector(q);
const slots = [
  ['chinese', '中文', '汉', '汉字与全角标点'],
  ['latinSans', '无衬线英文', 'Aa', '常见界面中的英文与数字'],
  ['serif', '衬线英文', 'Ag', '阅读界面请求的衬线字体'],
  ['monospace', '等宽英文', '01', '终端与代码中的等宽字体'],
];
const weights = Array.from({ length: 9 }, (_, i) => (i + 1) * 100);
const isPreview = location.hostname === '127.0.0.1' && new URLSearchParams(location.search).get('preview') === '1';
const native = typeof window.ksu?.exec === 'function';
let serial = 0, busy = false, status, dirty = false, toastTimer;
let profile = { schemaVersion: 1, name: '我的字体组合', activation: 'manual-reboot', slots: Object.fromEntries(slots.map(([key]) => [key, { mode: 'system' }])) };
let fonts = [];
let selectedSlot = null, editorStep = 'font', libraryPage = 1;
const libraryPageSize = 4;

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
function toast(message) { clearTimeout(toastTimer); $('#toast').textContent = message; $('#toast').hidden = false; toastTimer = setTimeout(() => $('#toast').hidden = true, 5000); }
function humanMessage(message) { return message.replace(/chinese:/g, '中文：').replace(/latinSans:/g, '无衬线英文：').replace(/serif:/g, '衬线英文：').replace(/monospace:/g, '等宽英文：').replace(/OEM/g, '厂商').replace(/全局 Hook/g, '全局应用适配'); }
function validation(message, warnings = [], error = false) { const box = $('#validation'); box.replaceChildren(); box.classList.toggle('error', error); if (warnings?.length) { const details = document.createElement('details'); details.append(text('summary', humanMessage(message) + ' · 查看说明')); const list = document.createElement('ul'); warnings.forEach(w => list.append(text('li', humanMessage(w)))); details.append(list); box.append(details); } else box.append(text('p', humanMessage(message))); box.hidden = false; }
async function run(fn) { if (busy) return; busy = true; document.querySelectorAll('button, input, select').forEach(e => e.disabled = true); try { await fn(); } catch (e) { toast(e.message); validation(e.message, [], true); } finally { busy = false; document.querySelectorAll('button, input, select').forEach(e => e.disabled = !(native || isPreview)); } }
function confirmAction(title, message) { return new Promise(resolve => { const dialog = $('#confirm-dialog'); $('#dialog-title').textContent = title; $('#dialog-text').textContent = message; const finish = result => { dialog.close(); resolve(result); }; $('#dialog-ok').disabled = false; $('#dialog-cancel').disabled = false; $('#dialog-ok').onclick = () => finish(true); $('#dialog-cancel').onclick = () => finish(false); dialog.oncancel = e => { e.preventDefault(); finish(false); }; dialog.showModal(); }); }
function faceList(key, mode, italic = false) { return fonts.flatMap(a => a.faces.filter(f => (key === 'chinese' ? f.chinese : f.latin) && f.italic === italic && (mode === 'variable' ? f.axes.length > 0 && f.axes.some(x => x.tag === 'wght') : f.axes.length === 0)).map(f => ({ asset: a, face: f, value: `${a.id}:${f.index}` }))); }
function faceMeta(id, index = 0) { return fonts.find(f => f.id === id)?.faces.find(f => f.index === index); }
function option(value, label) { const e = text('option', label); e.value = value; return e; }
function selectField(label, options, value, change) { const wrap = text('label', '', 'field'); wrap.append(text('span', label)); const select = document.createElement('select'); select.setAttribute('aria-label', label); options.forEach(([v, l]) => select.append(option(v, l))); select.value = value; select.addEventListener('change', () => { change(select.value); dirty = true; }); wrap.append(select); return wrap; }
function defaultMap(slot) { const a = faceMeta(slot.assetId, slot.faceIndex)?.axes.find(a => a.tag === 'wght'); return Object.fromEntries(weights.map(w => [w, a ? Math.max(a.min, Math.min(a.max, w)) : w])); }
function setRef(slot, value, italic = false) { const [id, index] = value.split(':'); if (italic) { if (id) { slot.italicAssetId = id; slot.italicFaceIndex = Number(index); } else { delete slot.italicAssetId; delete slot.italicFaceIndex; } } else { slot.assetId = id; slot.faceIndex = Number(index || 0); slot.axes = {}; delete slot.savedWeightMap; if (slot.familyMode === 'instances') slot.weightMap = defaultMap(slot); } }
function slotSummary(s) { if (s.mode === 'system') return '沿用系统字体'; if (s.mode === 'variable') return faceMeta(s.assetId, s.faceIndex)?.name || '选择一个可变字体'; return s.faces.length ? `静态多字重 · ${s.faces.length} 个字重面` : '选择静态字重文件'; }
function renderSlots(openKey) {
  if (openKey !== undefined) selectedSlot = openKey;
  $('#settings-overview').hidden = selectedSlot !== null;
  $('#font-page').hidden = selectedSlot === null;
  $('#settings-footnote').hidden = selectedSlot !== null;
  document.body.classList.toggle('editing-font', selectedSlot !== null);
  if (selectedSlot) {
    const index = slots.findIndex(([key]) => key === selectedSlot);
    $('#font-page-title').textContent = slots[index][1];
    $('#font-page-position').textContent = `${index + 1} / ${slots.length} 个分类`;
    $('#step-label').textContent = editorStep === 'font' ? '1 / 2 · 选择字体' : '2 / 2 · 字重设置';
    $('#step-previous').hidden = editorStep === 'font';
    $('#step-next').hidden = editorStep === 'weight';
  }
  const root = $('#slots'); root.replaceChildren();
  for (const [key, title, symbol, description] of slots) {
    if (selectedSlot && selectedSlot !== key) continue;
    const s = profile.slots[key];
    const card = text('section', '', 'slot-card'); card.dataset.slot = key;
    if (!selectedSlot) {
      const summary = text('button', '', 'slot-summary'); summary.type = 'button';
      const heading = text('div', '', 'slot-title'); const copy = text('div', '', 'slot-copy');
      copy.append(text('strong', title), text('small', slotSummary(s)));
      heading.append(text('span', symbol, 'slot-symbol'), copy); summary.append(heading);
      summary.onclick = () => { editorStep = 'font'; renderSlots(key); window.scrollTo(0, 0); };
      card.append(summary); root.append(card); continue;
    }
    const editor = text('div', '', 'slot-editor');
    editor.append(text('p', editorStep === 'font' ? description : slotSummary(s), 'field-help'));
    if (editorStep === 'font') editor.append(selectField('字体类型', [['system', '沿用系统'], ['variable', '可变字体'], ['static', '静态多字重']], s.mode, mode => {
      profile.slots[key] = mode === 'system' ? { mode } : mode === 'variable' ? { mode, assetId: '', faceIndex: 0, familyMode: 'instances', weightMap: Object.fromEntries(weights.map(w => [w, w])) } : { mode, missingWeightPolicy: 'nearest', faces: [] }; renderSlots(key);
    }));
    if (s.mode === 'variable') {
      if (editorStep === 'font') {
        const options = faceList(key, 'variable');
        editor.append(selectField('正体文件', [['', options.length ? '请选择字体' : '请先在字体库导入可变字体'], ...options.map(o => [o.value, o.face.name])], s.assetId ? `${s.assetId}:${s.faceIndex || 0}` : '', value => { setRef(s, value); renderSlots(key); }));
        const italics = faceList(key, 'variable', true);
        editor.append(selectField('斜体文件', [['', '未指定（由应用决定倾斜效果）'], ...italics.map(o => [o.value, o.face.name])], s.italicAssetId ? `${s.italicAssetId}:${s.italicFaceIndex || 0}` : '', value => setRef(s, value, true)));
        editor.append(text('p', '选择完成后，点“下一页”调整字重。', 'field-help'));
      } else {
        editor.append(selectField('字重方式', [['instances', '自定义九档映射'], ['continuous', '按应用请求连续变化']], s.familyMode, mode => { changeWeightMode(s, mode, defaultMap); renderSlots(key); }));
        const meta = faceMeta(s.assetId, s.faceIndex); const wght = meta?.axes.find(a => a.tag === 'wght');
        if (wght) editor.append(text('p', `文件轴范围 ${wght.min}–${wght.max}，默认 ${wght.default}。下方填写该文件的实际轴值。`, 'field-help'));
        if (s.familyMode === 'instances') {
          const grid = text('div', '', 'weight-grid');
          for (const w of weights) { const item = text('div', '', 'weight-item'); const label = text('label', `请求 ${w}${w === 400 ? ' · 常规' : ''}`); const input = document.createElement('input'); input.type = 'number'; input.step = 'any'; input.value = s.weightMap[w]; input.setAttribute('aria-label', `${title}请求 ${w} 的实际轴值`); if (wght) { input.min = wght.min; input.max = wght.max; } input.oninput = () => { s.weightMap[w] = input.value === '' ? NaN : Number(input.value); dirty = true; }; label.append(input); item.append(label); grid.append(item); } editor.append(grid);
        } else editor.append(text('p', '按应用请求选取字重，例如常规 400 使用文件轴 400。要自行加粗或减细，请选择“自定义九档映射”。原来的九档设置会保留，切回后恢复；保存并提交后需手动重启。', 'field-help'));
        const axes = meta?.axes.filter(a => a.tag !== 'wght') || [];
        if (axes.length) {
          const advanced = document.createElement('details'); advanced.className = 'advanced-axes'; advanced.append(text('summary', '其他可变轴'));
          for (const axis of axes) { const label = text('label', '', 'field'); label.append(text('span', `${axis.tag} · ${axis.min}–${axis.max}`)); const input = document.createElement('input'); input.type = 'number'; input.step = 'any'; input.min = axis.min; input.max = axis.max; input.value = s.axes?.[axis.tag] ?? axis.default; input.oninput = () => { s.axes ||= {}; s.axes[axis.tag] = input.value === '' ? NaN : Number(input.value); dirty = true; }; label.append(input); advanced.append(label); } editor.append(advanced);
        }
      }
    }
    if (s.mode === 'static') {
      if (editorStep === 'font') {
        const count = faceList(key, 'static').length + faceList(key, 'static', true).length;
        editor.append(text('p', `字体库有 ${count} 个可选的静态字体面。下一页按字重指定文件；同一个字体集合中的各个字体面也可以分别选择。`, 'field-help'));
      } else {
        editor.append(selectField('缺少字重时', [['nearest', '使用最近的已有字重'], ['reject', '必须提供完整九档正体']], s.missingWeightPolicy, value => s.missingWeightPolicy = value));
        const table = document.createElement('table'); table.className = 'weight-table'; const head = document.createElement('thead'); const tr = document.createElement('tr'); ['请求', '正体文件', '斜体文件'].forEach(t => tr.append(text('th', t))); head.append(tr); table.append(head); const body = document.createElement('tbody');
        for (const weight of weights) { const row = document.createElement('tr'); row.append(text('td', weight)); for (const style of ['normal', 'italic']) { const cell = document.createElement('td'); const sel = document.createElement('select'); sel.setAttribute('aria-label', `${title} ${weight} ${style === 'normal' ? '正体' : '斜体'}`); sel.append(option('', '未指定')); faceList(key, 'static', style === 'italic').forEach(o => sel.append(option(o.value, `${o.face.weight} · ${o.face.name}`))); const ref = s.faces.find(r => r.weight === weight && r.style === style); sel.value = ref ? `${ref.assetId}:${ref.faceIndex}` : ''; sel.onchange = () => { s.faces = s.faces.filter(r => r.weight !== weight || r.style !== style); if (sel.value) { const [assetId, index] = sel.value.split(':'); s.faces.push({ assetId, faceIndex: Number(index), weight, style }); } dirty = true; }; cell.append(sel); row.append(cell); } body.append(row); } table.append(body); editor.append(table);
      }
    }
    if (s.mode === 'system' && editorStep === 'weight') editor.append(text('p', '当前沿用系统字体，没有需要修改的字重。可回到上一页选择自己的字体。', 'field-help'));
    card.append(editor); root.append(card);
  }
}
function renderLibrary() {
  const root = $('#font-list'); root.replaceChildren(); $('#font-count').textContent = fonts.length;
  $('#library-pages').hidden = fonts.length === 0;
  if (!fonts.length) { root.append(text('div', '还没有导入字体。\n先添加一个字体文件，再回到“字体设置”选择它。', 'empty')); return; }
  const pageCount = Math.max(1, Math.ceil(fonts.length / libraryPageSize)); libraryPage = Math.min(pageCount, libraryPage);
  $('#library-page-label').textContent = `${libraryPage} / ${pageCount} 页`; $('#library-prev').hidden = libraryPage === 1; $('#library-next').hidden = libraryPage === pageCount;
  for (const font of fonts.slice((libraryPage - 1) * libraryPageSize, libraryPage * libraryPageSize)) {
    const item = text('article', '', 'font-entry'); const header = text('div', '', 'font-header'); header.append(text('h3', font.label)); const del = text('button', '移除'); del.onclick = async () => { if (await confirmAction('移除字体', `从模块字体库移除“${font.label}”？正在使用的字体会由模块保留。`)) run(async () => { await request({ op: 'delete_font', id: font.id }); await loadStatus(true); toast('已从字体库移除'); }); }; header.append(del); item.append(header);
    item.append(text('p', `${(font.size / 1048576).toFixed(1)} MiB · ${font.faces.length} 个字体面`, 'font-meta'));
    const faceContainer = font.faces.length > 1 ? document.createElement('details') : item; if (font.faces.length > 1) { faceContainer.append(text('summary', `查看 ${font.faces.length} 个字体面`)); item.append(faceContainer); }
    for (const f of font.faces) { const axes = f.axes.length ? `可变 ${f.axes.map(a => `${a.tag} ${a.min}–${a.max}`).join(' / ')}` : `静态 ${f.weight}`; faceContainer.append(text('p', `${f.name} · ${axes} · ${f.chinese ? '中文 ' : ''}${f.latin ? '英文' : ''}`)); }
    root.append(item);
  }
}
function renderStatus() {
  const active = status.active; const boot = status.boot; const pending = status.pending;
  $('#version').textContent = status.version; $('#status-dot').className = 'status-dot';
  if (boot.state === 'guarded') { $('#status-title').textContent = '已暂停这份字体配置'; $('#status-description').textContent = boot.error; $('#status-dot').classList.add('failed'); }
  else if (boot.state === 'failed') { $('#status-title').textContent = '本次挂载未完成'; $('#status-description').textContent = boot.error || '请查看诊断信息'; $('#status-dot').classList.add('failed'); }
  else if (status.visible) { $('#status-title').textContent = '字体覆盖已挂载'; $('#status-description').textContent = `${active?.profile.name || ''} · 已核对字体表和文件内容，界面效果仍需检查。`; }
  else if (boot.state === 'mounted') { $('#status-title').textContent = '覆盖状态待核对'; $('#status-description').textContent = status.visibilityError || '当前未读到完整的覆盖配置，请查看诊断或恢复系统字体后重试。'; }
  else { $('#status-title').textContent = boot.state === 'system' ? '当前沿用系统字体' : '尚未应用字体配置'; $('#status-description').textContent = '选择字体并保存后，手动重启即可尝试新配置。'; }
  $('#pending-description').hidden = !pending;
  if (pending) { const blocked = boot.state === 'guarded' && pending.id === boot.generation; $('#pending-description').textContent = `${blocked ? '已暂停，需重新提交' : '待重启使用'}：${pending.profile.name}`; if (!['failed', 'guarded'].includes(boot.state)) $('#status-dot').classList.add('pending'); }
  const dl = $('#diagnostic-details'); dl.replaceChildren(); for (const [label, value] of [['核心版本', status.version], ['Android SDK', status.baseline.sdk || '尚未识别'], ['挂载方式', '开机临时覆盖'], ['最近开机结果', boot.state || '尚未运行'], ['验证层级', status.visible ? '字体表与文件内容校验' : '尚无当前挂载证据'], ['应用界面实测', '待用户重启后验收']]) dl.append(text('dt', label), text('dd', value));
  const compat = status.fontCompatibility;
  if (compat?.needed) dl.append(text('dt', '小米默认字重'), text('dd', compat.preparedThisBoot ? '兼容层已准备；需启用 Zygisk，应用效果待验收' : '请启用 Zygisk，并手动重启以加载兼容层'));
}
async function loadStatus(keepDraft = false) { $('#validation').hidden = true; status = await request({ op: 'status' }); fonts = status.library || []; if (!keepDraft) { profile = structuredClone(status.draft); dirty = false; $('#profile-name').value = profile.name; } renderStatus(); renderSlots(); renderLibrary(); }
function currentProfile() { profile.name = $('#profile-name').value.trim(); const encoded = JSON.stringify(profile); if (encoded.includes(':null')) throw new Error('请填写所有字重与轴值'); return profile; }
document.querySelectorAll('nav button').forEach(button => button.onclick = () => { document.querySelectorAll('nav button').forEach(b => b.removeAttribute('aria-current')); button.setAttribute('aria-current', 'page'); document.body.dataset.tab = button.dataset.tab; document.querySelectorAll('.tab-panel').forEach(p => p.hidden = p.id !== button.dataset.tab); });
$('#profile-name').oninput = () => dirty = true;
$('#back-overview').onclick = () => { renderSlots(null); window.scrollTo(0, 0); };
$('#step-next').onclick = () => { editorStep = 'weight'; renderSlots(); window.scrollTo(0, 0); };
$('#step-previous').onclick = () => { editorStep = 'font'; renderSlots(); window.scrollTo(0, 0); };
$('#library-next').onclick = () => { libraryPage++; renderLibrary(); window.scrollTo(0, 0); };
$('#library-prev').onclick = () => { libraryPage--; renderLibrary(); window.scrollTo(0, 0); };
for (const key of ['coverage', 'runtime']) $('#show-' + key).onclick = () => { $('#coverage-page').hidden = key !== 'coverage'; $('#runtime-page').hidden = key !== 'runtime'; $('#show-coverage').removeAttribute('aria-current'); $('#show-runtime').removeAttribute('aria-current'); $('#show-' + key).setAttribute('aria-current', 'page'); };

$('#refresh').onclick = () => run(async () => { if (!dirty || await confirmAction('重新读取配置', '这会放弃尚未保存的编辑。')) await loadStatus(); });
$('#validate').onclick = () => run(async () => { const r = await request({ op: 'validate', profile: currentProfile() }); validation('配置检查通过', r.warnings); });
$('#save').onclick = () => run(async () => { const r = await request({ op: 'save', profile: currentProfile() }); dirty = false; validation('草稿已保存，尚未提交生效', r.warnings); toast('草稿已保存'); });
$('#stage').onclick = () => run(async () => { const r = await request({ op: 'stage', profile: currentProfile() }); dirty = false; selectedSlot = null; await loadStatus(); validation('已保存待生效配置。请手动重启后检查通知、设置和常用应用。', r.warnings); toast('已保存，等待你手动重启'); });
$('#restore').onclick = async () => { if (await confirmAction('恢复系统字体', '将保存一份系统字体配置，手动重启后撤销本模块的覆盖。字体库仍保留。')) run(async () => { await request({ op: 'restore' }); await loadStatus(); toast('已准备恢复，手动重启后生效'); }); };
$('#cancel-pending').onclick = () => run(async () => { await request({ op: 'cancel_pending' }); await loadStatus(true); toast('已取消待生效配置'); });
function downloadJSON(name, data) { const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })); const a = document.createElement('a'); a.href = url; a.download = name; a.click(); setTimeout(() => URL.revokeObjectURL(url), 10000); }
$('#export').onclick = () => run(async () => { if (native) { const r = await request({ op: 'export_diagnostics' }); toast(`已保存到 ${r.path}`); } else downloadJSON('HyperFont-diagnostics.json', await request({ op: 'diagnostics' })); });
$('#export-profile').onclick = () => run(async () => { const p = currentProfile(); await request({ op: 'validate', profile: p }); if (native) { const r = await request({ op: 'export_profile', profile: p }); toast(`已保存到 ${r.path}`); } else downloadJSON('HyperFont-profile.json', p); });
$('#profile-file').onchange = event => run(async () => { const file = event.target.files[0]; if (!file) return; try { if (file.size > 65536) throw new Error('配置文件过大'); const p = JSON.parse(await file.text()); await request({ op: 'save', profile: p }); await loadStatus(); toast('配置已导入为草稿，请检查后提交'); } finally { $('#profile-file').value = ''; } });
$('#refresh-baseline').onclick = async () => { if (await confirmAction('重新识别系统', '适用于系统升级后。本模块仍在覆盖字体时，请先恢复系统字体并重启。')) run(async () => { await request({ op: 'refresh_baseline' }); await loadStatus(true); toast('已识别当前系统，请重新提交配置'); }); };
$('#import-path-button').onclick = () => run(async () => { await request({ op: 'import_path', path: $('#import-path').value.trim() }); await loadStatus(true); toast('字体已导入'); });
$('#cleanup-uploads').onclick = async () => { if (busy) return; if (await confirmAction('清理未完成导入', '清理未完成上传的临时文件。其他面板中正在进行的导入也会取消；已导入的字体和当前设置保留。')) run(async () => { const r = await request({ op: 'cleanup_uploads' }); toast(`已清理 ${r.removed} 项未完成导入`); }); };
$('#font-file').onchange = event => run(async () => {
  const files = [...event.target.files]; $('#import-progress').hidden = false;
  try { for (const file of files) {
    if (file.size > 256 * 1048576) throw new Error('单个字体不能超过 256 MiB');
    let upload;
    try { upload = await request({ op: 'upload_begin', size: file.size, label: file.name });
      for (let offset = 0; offset < file.size; offset += 48 * 1024) { const chunk = new Uint8Array(await file.slice(offset, offset + 48 * 1024).arrayBuffer()); await request({ op: 'upload_chunk', token: upload.token, offset, chunk: base64(chunk) }); $('#import-meter').value = Math.round((offset + chunk.length) / file.size * 100); $('#import-message').textContent = `${file.name} · ${$('#import-meter').value}%`; }
      await request({ op: 'upload_finish', token: upload.token });
    } catch (e) { if (upload) { try { await request({ op: 'upload_cancel', token: upload.token }); } catch { /* A later import can retry; never hide original failure. */ } } throw e; }
  } await loadStatus(true); toast(`已导入 ${files.length} 个字体文件`);
  } finally { $('#font-file').value = ''; $('#import-progress').hidden = true; }
});
$('#preview-banner').hidden = !isPreview;
if (!native && !isPreview) { $('#offline').hidden = false; $('#status-title').textContent = '尚未连接模块'; $('#status-description').textContent = '从模块管理器进入 WebUI 后可读取手机配置。'; renderSlots(); renderLibrary(); document.querySelectorAll('button,input,select').forEach(e => e.disabled = true); document.querySelectorAll('nav button').forEach(e => e.disabled = false); }
else run(() => loadStatus());
