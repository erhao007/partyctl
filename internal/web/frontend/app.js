const state = { token: localStorage.getItem('partyctl_token') || '', esTraffic: null, esLogs: null };

const $ = (s, el=document) => el.querySelector(s);
const $$ = (s, el=document) => [...el.querySelectorAll(s)];

function headers(json=true) {
  const h = {};
  if (json) h['Content-Type'] = 'application/json';
  if (state.token) h['Authorization'] = 'Bearer ' + state.token;
  return h;
}

async function api(path, opts={}) {
  const res = await fetch(path, { ...opts, headers: { ...headers(opts.body !== undefined), ...(opts.headers||{}) } });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
  if (!res.ok) throw new Error((data && data.error) || res.statusText || 'request failed');
  return data;
}

function showApp(ok) {
  $('#login-view').classList.toggle('hidden', ok);
  $('#app').classList.toggle('hidden', !ok);
}

async function login(token) {
  await api('/api/login', { method: 'POST', body: JSON.stringify({ token }) });
  state.token = token;
  localStorage.setItem('partyctl_token', token);
  showApp(true);
  await boot();
}

async function boot() {
  // EventSource cannot set Authorization; ensure cookie is set.
  if (state.token) {
    try { await api('/api/login', { method: 'POST', body: JSON.stringify({ token: state.token }) }); } catch {}
  }
  await refreshStatus();
  await loadSettings();
  await loadProcProxy();
  await loadSubs();
  await loadOverrides();
  await loadProxies();
  connectTraffic();
  // logs connect when tab opens
}

function kernelLabel(s) {
  if (s.mihomo && (s.mihomo.version || s.mihomo.meta)) return s.mihomo.version || s.mihomo.meta;
  if (s.core_installed) return s.core_version || '已安装';
  return '未安装';
}

function statusHint(s) {
  if (!s.core_installed) return '内核未安装。请在服务器执行 partyctl core install，完成后回到这里点「启动」。';
  if (!s.current_profile) return '还没有订阅。打开「订阅」页添加并启用，然后点右上角「启动」。';
  if (!s.running) return '内核未运行。确认订阅已启用后，点右上角「启动」。';
  return '';
}

async function refreshStatus() {
  const s = await api('/api/status');
  const badge = $('#run-badge');
  badge.textContent = s.running ? '内核运行中' : '内核未运行';
  badge.className = 'badge ' + (s.running ? 'on' : 'off');
  $('#status-dl').innerHTML = [
    ['内核状态', s.running ? '运行中' : '未运行'],
    ['当前配置', s.current_profile || '—'],
    ['模式', s.mode],
    ['mixed-port', s.mixed_port],
    ['控制器', s.external_controller],
    ['TUN', s.tun ? '开' : '关'],
    ['内核', kernelLabel(s)],
  ].map(([k,v]) => `<dt>${k}</dt><dd>${v}</dd>`).join('');
  const hint = $('#status-hint');
  hint.textContent = statusHint(s);
  hint.classList.toggle('hidden', !hint.textContent);
  $$('.mode').forEach(b => b.classList.toggle('active', b.dataset.mode === s.mode));
}

function fmtBytes(n) {
  if (n == null || isNaN(n)) return '—';
  const u = ['B','KB','MB','GB','TB'];
  let i = 0; let x = Number(n);
  while (x >= 1024 && i < u.length-1) { x/=1024; i++; }
  return x.toFixed(i?1:0) + ' ' + u[i];
}

function connectTraffic() {
  if (state.esTraffic) state.esTraffic.close();
  const url = '/api/traffic';
  // EventSource can't set Authorization; use fetch stream fallback via cookie after login
  state.esTraffic = new EventSource(url);
  state.esTraffic.onmessage = (ev) => {
    try {
      const j = JSON.parse(ev.data);
      $('#traffic').textContent = `↑ ${fmtBytes(j.up)}/s  ↓ ${fmtBytes(j.down)}/s`;
    } catch {}
  };
  state.esTraffic.onerror = () => {
    // silently ignore when core down
  };
}

function connectLogs() {
  if (state.esLogs) state.esLogs.close();
  const view = $('#log-view');
  state.esLogs = new EventSource('/api/logs');
  state.esLogs.onmessage = (ev) => {
    view.textContent += ev.data + '\n';
    if (view.textContent.length > 200000) view.textContent = view.textContent.slice(-150000);
    view.scrollTop = view.scrollHeight;
  };
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

async function loadProxies() {
  const box = $('#groups');
  box.innerHTML = '<p class="muted">加载中…</p>';
  try {
    const st = await api('/api/status');
    if (!st.running) {
      box.innerHTML = '<p class="muted">内核未运行，请先点右上角「启动」。</p>';
      return;
    }
    const data = await api('/api/proxies');
    const pm = data.proxies || {};
    const groups = Object.entries(pm).filter(([,v]) => v && Array.isArray(v.all));
    if (!groups.length) { box.innerHTML = '<p class="muted">没有策略组。请确认订阅里有 proxy-groups。</p>'; return; }
    box.innerHTML = '';
    for (const [name, g] of groups.sort((a,b)=>a[0].localeCompare(b[0]))) {
      const el = document.createElement('div');
      el.className = 'group';
      el.innerHTML = `<div class="group-head"><strong>${name}</strong>
        <div class="row"><span class="muted">${g.type||''} · 当前 ${g.now||'—'}</span>
        <button class="btn btn-ghost" data-delay-group="${name}">测速</button></div></div>
        <div class="nodes"></div>`;
      const nodes = $('.nodes', el);
      for (const n of g.all) {
        const b = document.createElement('button');
        b.className = 'node' + (n === g.now ? ' now' : '');
        b.innerHTML = `${n}<span class="delay" data-d="${name}::${n}"></span>`;
        b.onclick = async () => {
          await api('/api/proxies/' + encodeURIComponent(name), { method:'PUT', body: JSON.stringify({ name: n }) });
          await loadProxies();
        };
        nodes.appendChild(b);
      }
      box.appendChild(el);
    }
    $$('[data-delay-group]').forEach(btn => btn.onclick = async () => {
      const g = btn.dataset.delayGroup;
      btn.disabled = true; btn.textContent = '测速中…';
      try {
        const delays = await api('/api/delay/' + encodeURIComponent(g));
        for (const [n, d] of Object.entries(delays)) {
          const span = document.querySelector(`[data-d="${CSS.escape(g)}::${CSS.escape(n)}"]`);
          // CSS.escape may not like :: — fallback query
          const all = document.querySelectorAll('.delay');
          all.forEach(s => {
            if (s.getAttribute('data-d') === g + '::' + n) {
              s.textContent = typeof d === 'number' ? ` ${d}ms` : '';
            }
          });
        }
      } catch (e) { alert(e.message); }
      finally { btn.disabled = false; btn.textContent = '测速'; }
    });
  } catch (e) {
    const msg = e.message || '加载失败';
    if (msg.includes('内核未运行')) {
      box.innerHTML = '<p class="muted">内核未运行，请先点右上角「启动」。</p>';
      return;
    }
    box.innerHTML = `<p class="error">${esc(msg)}</p>`;
  }
}

async function loadSubs() {
  const data = await api('/api/subs');
  const box = $('#subs-list');
  const profiles = data.profiles || [];
  if (!profiles.length) { box.innerHTML = '<p class="muted">暂无订阅。填写名称和订阅地址后点「添加」。</p>'; return; }
  box.innerHTML = profiles.map(p => {
    const u = p.userinfo || {};
    const traffic = (u.total || u.used) ? `流量 ${fmtBytes(u.used||0)} / ${fmtBytes(u.total||0)}` : '';
    const exp = u.expire_at ? `到期 ${u.expire_at}` : '';
    return `<div class="sub-item ${p.current?'current':''}">
      <div class="row between"><strong>${p.name}${p.current?' · 当前':''}</strong><span class="muted">${p.updated||''}</span></div>
      <div class="muted" style="font-size:12px;word-break:break-all">${p.url||'(本地)'}</div>
      <div class="muted" style="margin-top:6px">${[traffic,exp].filter(Boolean).join(' · ')||'无 subscription-userinfo'}</div>
      <div class="sub-actions">
        <button class="btn btn-ghost" data-use="${p.name}">启用</button>
        <button class="btn btn-ghost" data-upd="${p.name}" ${p.url?'':'disabled'}>更新</button>
        <button class="btn btn-danger" data-del="${p.name}">删除</button>
      </div>
    </div>`;
  }).join('');
  $$('[data-use]').forEach(b => b.onclick = async () => { await api('/api/subs/'+encodeURIComponent(b.dataset.use)+'/use', {method:'POST'}); await loadSubs(); await refreshStatus(); });
  $$('[data-upd]').forEach(b => b.onclick = async () => { await api('/api/subs/'+encodeURIComponent(b.dataset.upd)+'/update', {method:'POST'}); await loadSubs(); });
  $$('[data-del]').forEach(b => b.onclick = async () => {
    if (!confirm('删除 '+b.dataset.del+'？')) return;
    await api('/api/subs/'+encodeURIComponent(b.dataset.del), {method:'DELETE'});
    await loadSubs();
  });
}

async function loadOverrides() {
  const data = await api('/api/overrides');
  const box = $('#ov-list');
  box.innerHTML = (data.overrides||[]).map(o => `<button type="button" class="chip" data-ov="${o.name}">${o.name}</button>`).join('') || '<span class="muted">暂无覆盖</span>';
  $$('[data-ov]').forEach(b => b.onclick = async () => {
    $$('.chip').forEach(c => c.classList.remove('active'));
    b.classList.add('active');
    const d = await api('/api/overrides/' + encodeURIComponent(b.dataset.ov));
    $('#ov-name').value = d.name;
    $('#ov-content').value = d.content || '';
  });
}

async function loadSettings() {
  const s = await api('/api/settings');
  const f = $('#settings-form');
  f.mixed_port.value = s.mixed_port;
  f.mode.value = s.mode;
  f.log_level.value = s.log_level || 'info';
  f.tun.checked = !!s.tun;
  f.allow_lan.checked = !!s.allow_lan;
  f.external_controller.value = s.external_controller || '';
  f.web_listen.value = (s.web && s.web.listen) || '127.0.0.1:9800';
  f.web_enabled.checked = !!(s.web && s.web.enabled);
}

async function loadProcProxy() {
  const box = $('#proc-proxy-status');
  const list = $('#proc-proxy-list');
  if (!box || !list) return;
  try {
    const s = await api('/api/proc-proxy');
    const n = (s.procs || []).length;
    box.textContent = (s.enabled ? '已开启' : '未开启') + (s.rules ? '，规则已安装' : '，规则未安装') + '，' + n + ' 个进程';
    list.innerHTML = (s.procs || []).map(p => `<li>${p.pid} ${esc(p.cmd || '')} <button type="button" class="btn btn-ghost" data-rm="${p.pid}">撤销</button></li>`).join('') || '<li class="muted">当前没有接管任何进程</li>';
    $$('[data-rm]', list).forEach(b => b.onclick = async () => {
      try {
        await api('/api/proc-proxy?pid=' + encodeURIComponent(b.dataset.rm), { method: 'DELETE' });
        await loadProcProxy();
      } catch (err) { alert(err.message); }
    });
  } catch (err) {
    box.textContent = err.message;
  }
}

// events
$('#login-form').onsubmit = async (e) => {
  e.preventDefault();
  $('#login-error').textContent = '';
  try { await login($('#login-token').value.trim()); }
  catch (err) { $('#login-error').textContent = err.message; }
};

$('#logout').onclick = () => {
  state.token = '';
  localStorage.removeItem('partyctl_token');
  document.cookie = 'partyctl_token=; Max-Age=0; path=/';
  if (state.esTraffic) state.esTraffic.close();
  if (state.esLogs) state.esLogs.close();
  showApp(false);
};

$$('[data-core]').forEach(b => b.onclick = async () => {
  b.disabled = true;
  try {
    await api('/api/core/' + b.dataset.core, { method: 'POST' });
    await refreshStatus();
    setTimeout(refreshStatus, 800);
  } catch (e) { alert(e.message); }
  finally { b.disabled = false; }
});

$$('.tab').forEach(t => t.onclick = () => {
  $$('.tab').forEach(x => x.classList.remove('active'));
  t.classList.add('active');
  $$('.panel').forEach(p => p.classList.add('hidden'));
  $('#tab-' + t.dataset.tab).classList.remove('hidden');
  if (t.dataset.tab === 'logs' && !state.esLogs) connectLogs();
  if (t.dataset.tab === 'nodes') loadProxies();
  if (t.dataset.tab === 'settings') loadProcProxy();
});

$$('.mode').forEach(b => b.onclick = async () => {
  try {
    await api('/api/settings', { method:'PUT', body: JSON.stringify({ mode: b.dataset.mode }) });
    await refreshStatus();
  } catch (e) { alert(e.message); }
});

$('#refresh-proxies').onclick = loadProxies;
$('#clear-logs').onclick = () => { $('#log-view').textContent = ''; };

$('#add-sub').onsubmit = async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  try {
    await api('/api/subs', { method:'POST', body: JSON.stringify({ name: fd.get('name'), url: fd.get('url') }) });
    e.target.reset();
    await loadSubs();
    await refreshStatus();
  } catch (err) { alert(err.message); }
};

$('#ov-form').onsubmit = async (e) => {
  e.preventDefault();
  const name = $('#ov-name').value.trim();
  try {
    await api('/api/overrides/' + encodeURIComponent(name), { method:'PUT', body: JSON.stringify({ content: $('#ov-content').value }) });
    await loadOverrides();
  } catch (err) { alert(err.message); }
};
$('#ov-delete').onclick = async () => {
  const name = $('#ov-name').value.trim();
  if (!name || !confirm('删除 '+name+'？')) return;
  await api('/api/overrides/' + encodeURIComponent(name), { method:'DELETE' });
  $('#ov-content').value = '';
  await loadOverrides();
};

$('#proc-proxy-add').onsubmit = async (e) => {
  e.preventDefault();
  const pid = Number(new FormData(e.target).get('pid'));
  try {
    await api('/api/proc-proxy', { method: 'POST', body: JSON.stringify({ pid }) });
    e.target.reset();
    await loadProcProxy();
  } catch (err) { alert(err.message); }
};
$('#proc-proxy-clear').onclick = async () => {
  if (!confirm('清理全部按进程代理规则？被接管的进程会恢复直连。')) return;
  try {
    await api('/api/proc-proxy/clear', { method: 'POST', body: '{}' });
    await loadProcProxy();
  } catch (err) { alert(err.message); }
};

$('#settings-form').onsubmit = async (e) => {
  e.preventDefault();
  const f = e.target;
  const body = {
    mixed_port: Number(f.mixed_port.value),
    mode: f.mode.value,
    log_level: f.log_level.value,
    tun: f.tun.checked,
    allow_lan: f.allow_lan.checked,
    external_controller: f.external_controller.value,
    web: { enabled: f.web_enabled.checked, listen: f.web_listen.value },
  };
  try {
    await api('/api/settings', { method:'PUT', body: JSON.stringify(body) });
    alert('已保存并重新生成');
    await refreshStatus();
  } catch (err) { alert(err.message); }
};

(async function init() {
  if (!state.token) { showApp(false); return; }
  try {
    // probe
    await api('/api/status');
    showApp(true);
    await boot();
  } catch {
    showApp(false);
  }
})();
