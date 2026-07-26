'use strict';

// ── Utilities ─────────────────────────────────────────────────────────────────

const $ = (sel, ctx = document) => ctx.querySelector(sel);
const $$ = (sel, ctx = document) => [...ctx.querySelectorAll(sel)];

function esc(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function relTime(iso) {
  if (!iso) return '—';
  const secs = Math.floor((Date.now() - new Date(iso)) / 1000);
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
  return new Date(iso).toLocaleDateString();
}

function fmtDur(ms) {
  if (!ms || ms <= 0) return '';
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
}

function fmtSignalType(t) { return (t || '').replace(/_/g, ' '); }

function chip(type) {
  return `<span class="chip chip-${esc(type)}">${esc(fmtSignalType(type))}</span>`;
}
function badge(status) {
  return `<span class="badge badge-${esc(status)}">${esc(status)}</span>`;
}

// ── Error banner ──────────────────────────────────────────────────────────────

function showBanner(msg, type = 'error') {
  let banner = $('#global-banner');
  if (!banner) {
    banner = document.createElement('div');
    banner.id = 'global-banner';
    document.body.prepend(banner);
  }
  banner.className = `global-banner global-banner-${type}`;
  banner.innerHTML = `<span>${esc(msg)}</span>
    <button onclick="this.parentElement.remove()" class="banner-close">×</button>`;
}

// ═════════════════════════════════════════════════════════════════════════════
// DASHBOARD PAGE
// ═════════════════════════════════════════════════════════════════════════════

async function initDashboard() {
  await loadRuns();
}

async function loadRuns() {
  const tbody = $('#runs-tbody');
  if (!tbody) return;
  try {
    const res = await fetch('/runs');
    if (!res.ok) throw new Error(res.statusText);
    const runs = await res.json();

    if (!runs || !runs.length) {
      tbody.innerHTML = `<tr><td colspan="7" class="empty">
        No runs yet. <a href="/run.html">Start one →</a></td></tr>`;
      return;
    }

    tbody.innerHTML = runs.map(r => `
      <tr>
        <td>
          <div style="font-weight:600">${esc(r.name)}</div>
          <div class="small muted">${esc(r.title)}</div>
        </td>
        <td>${esc(r.company)}</td>
        <td>${badge(r.status)}</td>
        <td>
          ${r.hook_type ? chip(r.hook_type) : '<span class="muted">—</span>'}
          ${r.hook_title ? `<div class="small muted truncate mt-6">${esc(r.hook_title)}</div>` : ''}
        </td>
        <td>${badge(r.review_status)}</td>
        <td class="small muted">${relTime(r.started_at)}</td>
        <td class="text-right">
          ${r.status === 'completed'
            ? `<a href="/run.html?id=${esc(r.id)}&replay=1" class="btn btn-ghost btn-sm" title="Replay offline">⟳ Replay</a> `
            : ''}
          <a href="/run.html?id=${esc(r.id)}" class="btn btn-ghost btn-sm">View →</a>
        </td>
      </tr>
    `).join('');

    if (runs.some(r => r.status === 'running')) {
      setTimeout(loadRuns, 6000);
    }
  } catch (e) {
    const tbody2 = $('#runs-tbody');
    if (tbody2) tbody2.innerHTML = `<tr><td colspan="7" class="empty error-text">
      Error loading runs: ${esc(e.message)}</td></tr>`;
    showBanner('Could not load runs: ' + e.message);
  }
}

// ═════════════════════════════════════════════════════════════════════════════
// RUN PAGE
// ═════════════════════════════════════════════════════════════════════════════

const STAGES = [
  { name: 'Research',       label: 'Research' },
  { name: 'ExtractSignals', label: 'Extract Signals' },
  { name: 'SelectHook',     label: 'Select Hook' },
  { name: 'DraftMessage',   label: 'Draft Message' },
  { name: 'AwaitReview',    label: 'Await Review' },
];

let activeES = null;

async function initRunPage() {
  const params = new URLSearchParams(location.search);
  const id      = params.get('id');
  const replay  = params.get('replay') === '1';

  if (id) {
    await loadExistingRun(id, replay);
  } else {
    showForm();
  }
}

// ── Form ──────────────────────────────────────────────────────────────────────

function showForm() {
  $('#form-section').style.display = '';
  $('#pipeline-section').style.display = 'none';
  $('#prospect-form').addEventListener('submit', onSubmit);
}

async function onSubmit(e) {
  e.preventDefault();
  const form = e.target;
  const btn  = $('#submit-btn');
  btn.disabled = true;
  btn.textContent = 'Starting…';
  clearFormError();

  const prospect = {
    name:         form.querySelector('[name=name]').value.trim(),
    title:        form.querySelector('[name=title]').value.trim(),
    company:      form.querySelector('[name=company]').value.trim(),
    linkedin_url: form.querySelector('[name=linkedin]').value.trim() || undefined,
    notes:        form.querySelector('[name=notes]').value.trim() || undefined,
  };

  // Remove undefined keys (avoid sending nulls)
  Object.keys(prospect).forEach(k => prospect[k] === undefined && delete prospect[k]);

  try {
    const res = await fetch('/runs', {
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      body:    JSON.stringify(prospect),
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      if (res.status === 429) {
        showFormError('Rate limit reached — please wait a minute before starting another run.');
      } else if (res.status === 422 || res.status === 400) {
        showFormError('Validation error: ' + (body.error || res.statusText));
      } else {
        showFormError('Server error: ' + (body.error || res.statusText));
      }
      btn.disabled = false;
      btn.textContent = 'Run Pipeline →';
      return;
    }

    const { run_id } = await res.json();
    history.pushState({}, '', `/run.html?id=${run_id}`);
    mountPipelineView(run_id, prospect);
    openSSE(run_id, '/runs/' + run_id + '/stream');
  } catch (err) {
    showFormError('Network error: ' + err.message);
    btn.disabled = false;
    btn.textContent = 'Run Pipeline →';
  }
}

function showFormError(msg) {
  const el = $('#form-error');
  if (el) { el.textContent = msg; el.style.display = ''; }
}
function clearFormError() {
  const el = $('#form-error');
  if (el) { el.textContent = ''; el.style.display = 'none'; }
}

// ── Load existing run (live detail or replay) ─────────────────────────────────

async function loadExistingRun(id, replay) {
  try {
    const res = await fetch(`/runs/${id}`);
    if (!res.ok) { showError('Run not found.'); return; }
    const data = await res.json();

    // Show mode tag (live vs replay)
    mountPipelineView(id, data, data.status, replay);

    // Replay mode: open the replay SSE endpoint directly — same format, no API calls
    if (replay && data.status !== 'running') {
      openSSE(id, `/runs/${id}/replay`);
      return;
    }

    // Normal detail: replay stored stages instantly
    for (const st of (data.stages || [])) {
      applyStageEvent({
        stage:       st.stage_name,
        index:       st.stage_index,
        total:       5,
        status:      st.status,
        output:      st.output,
        reasoning:   st.reasoning,
        duration_ms: st.duration_ms,
        error:       st.error_msg,
      });
    }
    setRunBadge(data.status);

    if (data.draft_body) showDraftCard(data, id);

    if (data.status === 'running') {
      openSSE(id, `/runs/${id}/stream`);
    }
  } catch (e) {
    showError('Could not load run: ' + e.message);
  }
}

// ── Pipeline view mount ───────────────────────────────────────────────────────

function mountPipelineView(runId, prospect, currentStatus, isReplay) {
  $('#form-section').style.display = 'none';
  const ps = $('#pipeline-section');
  ps.style.display = '';

  $('#run-name').textContent     = prospect.name || '';
  $('#run-subtitle').textContent = `${prospect.title || ''} @ ${prospect.company || ''}`;

  // Show replay badge
  const modeBadge = $('#run-mode-badge');
  if (modeBadge) {
    if (isReplay) {
      modeBadge.textContent = 'REPLAY';
      modeBadge.style.display = '';
    } else {
      modeBadge.style.display = 'none';
    }
  }

  const container = $('#stages-container');
  container.innerHTML = STAGES.map((s, i) => stageCardHTML(i + 1, s)).join('');

  if (!currentStatus || currentStatus === 'running') {
    setStageState(1, 'running');
  }
}

function stageCardHTML(index, stage) {
  return `
    <div class="stage-card" id="sc-${index}" data-state="pending">
      <div class="stage-header">
        <div class="stage-title">
          <div class="stage-num">${index}</div>
          <span>${esc(stage.label)}</span>
        </div>
        <div class="stage-meta">
          <span id="sc-${index}-dur"></span>
          <span id="sc-${index}-icon"></span>
        </div>
      </div>
      <div class="stage-body" id="sc-${index}-body" style="display:none">
        <div class="stage-output" id="sc-${index}-out"></div>
        <details class="stage-reasoning" id="sc-${index}-rsn" style="display:none">
          <summary>Reasoning &amp; edge-case notes</summary>
          <pre id="sc-${index}-rsn-text"></pre>
        </details>
      </div>
    </div>`;
}

function setStageState(index, state) {
  const card = $(`#sc-${index}`);
  if (!card) return;
  card.dataset.state = state;
  const icon = $(`#sc-${index}-icon`);
  if (!icon) return;
  switch (state) {
    case 'running':  icon.innerHTML = '<span class="spinner"></span>'; break;
    case 'ok':       icon.textContent = '✓'; break;
    case 'degraded': icon.textContent = '⚡'; break;
    case 'failed':   icon.textContent = '✗'; break;
    default:         icon.textContent = ''; break;
  }
}

// ── Apply one SSE event ───────────────────────────────────────────────────────

function applyStageEvent(ev) {
  const idx = STAGES.findIndex(s => s.name === ev.stage) + 1;
  if (!idx) return;

  setStageState(idx, ev.status);

  const dur = $(`#sc-${idx}-dur`);
  if (dur) dur.textContent = fmtDur(ev.duration_ms);

  const body = $(`#sc-${idx}-body`);
  if (body) body.style.display = '';

  const out = $(`#sc-${idx}-out`);
  if (out) out.innerHTML = renderOutput(ev.stage, ev.output, ev.status, ev.error);

  if (ev.reasoning) {
    const rsn = $(`#sc-${idx}-rsn`);
    const rsnText = $(`#sc-${idx}-rsn-text`);
    if (rsn && rsnText) {
      rsn.style.display = '';
      rsnText.textContent = ev.reasoning;
    }
  }

  if (ev.status !== 'failed' && idx < 5) setStageState(idx + 1, 'running');
}

// ── Stage output renderers ────────────────────────────────────────────────────

function renderOutput(stageName, output, status, error) {
  let html = '';
  switch (stageName) {
    case 'Research':       html = renderResearch(output); break;
    case 'ExtractSignals': html = renderSignals(output);  break;
    case 'SelectHook':     html = renderHook(output);     break;
    case 'DraftMessage':   html = renderDraft(output, status); break;
    case 'AwaitReview':    html = renderReview(output);   break;
    default:
      html = output ? `<pre class="mono">${esc(JSON.stringify(output, null, 2))}</pre>` : '';
  }
  if (error) html += `<div class="small mt-6 error-text">⚠ ${esc(error.substring(0, 300))}</div>`;
  return html;
}

function renderResearch(o) {
  if (!o) return '';
  const count    = o.count || 0;
  const snippets = (o.snippets || []).slice(0, 4);
  if (!count) return `<div class="edge-case-banner">⚠ No public footprint — search returned 0 results. Fallback signal will be used.</div>`;
  return `
    <div class="out-stat">${count} snippet${count !== 1 ? 's' : ''} retrieved</div>
    <ul class="snippet-list">
      ${snippets.map(s => {
        const m   = s.match(/^\[([^\]]+)\]/);
        const url = m ? m[1] : '';
        const txt = s.replace(/^\[[^\]]+\]\s*/, '').substring(0, 130);
        return `<li><span class="s-url">${esc(url)}</span><span class="s-text">${esc(txt)}…</span></li>`;
      }).join('')}
      ${(o.snippets || []).length > 4
        ? `<li class="muted small">…and ${o.snippets.length - 4} more</li>`
        : ''}
    </ul>`;
}

function renderSignals(o) {
  const sigs = (o && o.signals) || [];
  const ec   = o && o.edge_cases;
  let html   = '';

  // Edge-case banners
  if (ec) {
    if (ec.no_footprint)          html += ecBanner('no_footprint',         ec.no_footprint);
    if (ec.stale_signals_removed) html += ecBanner('stale',                ec.stale_signals_removed);
    if (ec.conflicting_company)   html += ecBanner('conflicting_company',  ec.conflicting_company);
  }

  if (!sigs.length) return html || `<span class="muted small">No signals found — fallback in use</span>`;

  html += sigs.map(s => {
    const stale = s.summary && s.summary.startsWith('[STALE');
    return `<div class="signal-row ${stale ? 'signal-stale' : ''}">
      ${chip(s.type)}
      <span class="signal-title">${esc(s.title || '')}</span>
      <span class="signal-score">${Math.round((s.relevance_score || 0) * 100)}%</span>
    </div>`;
  }).join('');
  return html;
}

function renderHook(o) {
  if (!o || !o.hook) return '';
  const h = o.hook;
  let html = `
    <div class="signal-row">
      ${chip(h.signal.type)}
      <span class="signal-title"><strong>${esc(h.signal.title || '')}</strong></span>
    </div>
    ${h.signal.summary ? `<div class="small muted mt-6">${esc(h.signal.summary)}</div>` : ''}`;

  // Runner-up display for competing-signals demo
  if (o.runner_up) {
    html += `<div class="runner-up-box mt-12">
      <div class="runner-up-label">Runner-up <span class="chip chip-general">considered &amp; rejected</span></div>
      <div class="signal-row">
        ${chip(o.runner_up.type)}
        <span class="signal-title">${esc(o.runner_up.title || '')}</span>
        <span class="signal-score">${Math.round((o.runner_up.relevance_score || 0) * 100)}%</span>
      </div>
    </div>`;
  }
  return html;
}

function renderDraft(o, status) {
  if (!o || !o.draft) return '';
  const d = o.draft;
  return `
    <div class="draft-preview">
      <div class="draft-subj"><strong>Subject:</strong> ${esc(d.subject || '')}</div>
      <div class="draft-body-text mt-6">${esc(d.body || '')}</div>
    </div>
    ${status === 'degraded'
      ? `<div class="small muted mt-6">⚡ Generated using fallback template</div>`
      : ''}`;
}

function renderReview(o) {
  if (!o) return '';
  return `<div class="small muted">${esc(o.message || o.status || '')}</div>`;
}

function ecBanner(type, msg) {
  const icons = { no_footprint: '🔍', stale: '🕐', conflicting_company: '🔀' };
  return `<div class="edge-case-banner edge-case-${type}">${icons[type] || '⚠'} ${esc(msg)}</div>`;
}

// ── Draft review card ─────────────────────────────────────────────────────────

function showDraftCard(run, runId) {
  const sec = $('#draft-section');
  if (!sec || !run.draft_body) return;
  sec.style.display = '';
  sec.innerHTML = `
    <div class="review-card">
      <div class="review-card-header">
        <span>📋</span>
        <h2>Outreach Draft</h2>
        <span class="review-badge">Needs Human Review</span>
      </div>
      <div class="review-body">
        <div class="review-field">
          <label>Subject</label>
          <div class="review-subject">${esc(run.draft_subject || '')}</div>
        </div>
        <div class="review-field">
          <label>Message</label>
          <div class="review-body-text">${esc(run.draft_body || '')}</div>
        </div>
        ${run.hook_type ? `
        <div class="review-field">
          <label>Hook Used</label>
          <div class="flex gap-8" style="align-items:center">
            ${chip(run.hook_type)}
            <span class="small">${esc(run.hook_title || '')}</span>
          </div>
        </div>` : ''}
      </div>
      ${reviewActionsHTML(run.review_status, runId)}
    </div>`;
}

function reviewActionsHTML(status, runId) {
  if (status === 'approved')  return `<div class="review-done approved">✓ Approved — ready for sending (manual action required)</div>`;
  if (status === 'discarded') return `<div class="review-done discarded">✗ Draft discarded</div>`;
  return `<div class="review-actions">
    <button class="btn btn-success" onclick="submitReview('${esc(runId)}','approved')">✓ Approve Draft</button>
    <button class="btn btn-ghost"   onclick="submitReview('${esc(runId)}','discarded')">✗ Discard</button>
  </div>`;
}

async function submitReview(runId, action) {
  try {
    const res = await fetch(`/runs/${runId}/review`, {
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      body:    JSON.stringify({ action }),
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      showBanner('Review error: ' + (body.error || res.statusText));
      return;
    }
    const actionsEl = $('.review-actions');
    if (actionsEl) {
      actionsEl.outerHTML = action === 'approved'
        ? `<div class="review-done approved">✓ Approved — ready for sending (manual action required)</div>`
        : `<div class="review-done discarded">✗ Draft discarded</div>`;
    }
  } catch (e) {
    showBanner('Network error during review: ' + e.message);
  }
}

// ── SSE streaming ─────────────────────────────────────────────────────────────

function openSSE(runId, url) {
  if (activeES) activeES.close();
  activeES = new EventSource(url);

  activeES.onmessage = function(e) {
    let data;
    try { data = JSON.parse(e.data); } catch { return; }

    if (data.type === 'stage') {
      applyStageEvent(data);
      if (data.stage === 'DraftMessage' && data.output && data.output.draft) {
        showDraftCard({
          draft_subject: data.output.draft.subject,
          draft_body:    data.output.draft.body,
          hook_type:     null,
          hook_title:    null,
          review_status: 'pending',
        }, runId);
      }
    }

    if (data.type === 'done') {
      activeES.close();
      activeES = null;
      setRunBadge(data.status);
      if (data.status === 'completed') setTimeout(() => reloadDraftCard(runId), 600);
    }
  };

  activeES.onerror = function() {
    if (activeES && activeES.readyState === EventSource.CLOSED) {
      activeES = null;
    }
  };
}

async function reloadDraftCard(runId) {
  try {
    const res = await fetch(`/runs/${runId}`);
    if (!res.ok) return;
    const run = await res.json();
    if (run.draft_body) showDraftCard(run, runId);
  } catch (_) {}
}

function setRunBadge(status) {
  const el = $('#run-status-badge');
  if (el) { el.className = `badge badge-${status}`; el.textContent = status; }
}

// ── Error view ────────────────────────────────────────────────────────────────

function showError(msg) {
  $('#form-section').style.display = 'none';
  const ps = $('#pipeline-section');
  ps.style.display = '';
  ps.innerHTML = `
    <div class="card card-body" style="color:#dc2626;padding:24px">
      ${esc(msg)}<br>
      <a href="/run.html" class="btn btn-ghost mt-12">← Start a new run</a>
    </div>`;
}

// ── Init ──────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  const pid = document.body.dataset.page;
  if (pid === 'dashboard') initDashboard();
  else if (pid === 'run')  initRunPage();
});
