async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  return res.json();
}

async function createGroup() {
  const cfg = {
    name: document.getElementById('f-name').value,
    tenant_prefix: document.getElementById('f-prefix').value,
    tenant_count: parseInt(document.getElementById('f-tenants').value),
    destinations_per_tenant: parseInt(document.getElementById('f-dests').value),
    topics: document.getElementById('f-topics').value.split(',').map(s => s.trim()),
    publish: {
      rate_per_tenant: parseInt(document.getElementById('f-rate').value),
      pattern: 'constant',
      payload_bytes: parseInt(document.getElementById('f-payload').value),
    },
    mock_profile: {
      latency_ms: parseInt(document.getElementById('f-latency').value),
      latency_jitter_ms: parseInt(document.getElementById('f-jitter').value),
      error_rate: parseFloat(document.getElementById('f-error').value),
    },
  };
  await api('POST', '/api/groups', cfg);
  refresh();
}

function renderCard(g) {
  const m = g.metrics || {};
  return `
    <div class="card" data-group="${g.name}">
      <div class="card-header">
        <span class="card-name">${g.name}</span>
        <span class="card-state state-${g.state}">${g.state}</span>
      </div>
      <div class="card-config">${g.config.tenant_count} tenants · ${g.config.destinations_per_tenant} dests/tenant · ${g.config.publish.payload_bytes}B payload · ${g.config.topics.join(', ')}</div>
      <div class="metrics">
        <div class="metric"><div class="metric-value" data-m="pub_rate">${fmt(m.publish_rate_per_sec)}</div><div class="metric-label">pub/s</div></div>
        <div class="metric"><div class="metric-value" data-m="del_rate">${fmt(m.delivery_rate_per_sec)}</div><div class="metric-label">del/s</div></div>
        <div class="metric"><div class="metric-value" data-m="in_flight">${m.in_flight || 0}</div><div class="metric-label">in-flight</div></div>
        <div class="metric has-tooltip" title="E2E Latency\np50: ${m.e2e_latency_p50_ms||0}ms\np90: ${m.e2e_latency_p90_ms||0}ms\np95: ${m.e2e_latency_p95_ms||0}ms\np99: ${m.e2e_latency_p99_ms||0}ms\nmax: ${m.e2e_latency_max_ms||0}ms\n\nPublish Latency\np50: ${m.publish_latency_p50_ms||0}ms\np90: ${m.publish_latency_p90_ms||0}ms\np95: ${m.publish_latency_p95_ms||0}ms\np99: ${m.publish_latency_p99_ms||0}ms\nmax: ${m.publish_latency_max_ms||0}ms"><div class="metric-value" data-m="p50">${m.e2e_latency_p50_ms || 0}</div><div class="metric-label">p50 ms</div></div>
        <div class="metric has-tooltip" title="E2E Latency\np50: ${m.e2e_latency_p50_ms||0}ms\np90: ${m.e2e_latency_p90_ms||0}ms\np95: ${m.e2e_latency_p95_ms||0}ms\np99: ${m.e2e_latency_p99_ms||0}ms\nmax: ${m.e2e_latency_max_ms||0}ms"><div class="metric-value" data-m="p95">${m.e2e_latency_p95_ms || 0}</div><div class="metric-label">p95 ms</div></div>
        <div class="metric"><div class="metric-value" data-m="missing">${m.missing_total || 0}</div><div class="metric-label">missing</div></div>
        <div class="metric"><div class="metric-value" data-m="pub_total">${m.publish_total || 0}</div><div class="metric-label">pub total</div></div>
        <div class="metric"><div class="metric-value" data-m="del_total">${m.delivery_total || 0}</div><div class="metric-label">del total</div></div>
        <div class="metric"><div class="metric-value" data-m="errors">${m.publish_errors || 0}</div><div class="metric-label">errors</div></div>
      </div>
      <div class="controls">
        <button onclick="api('POST','/api/groups/${g.name}/provision')">Provision</button>
        <button class="primary" onclick="api('POST','/api/groups/${g.name}/start')">Start</button>
        <button onclick="api('POST','/api/groups/${g.name}/stop')">Stop</button>
        <button onclick="api('POST','/api/groups/${g.name}/reset')">Reset</button>
        <button class="danger" onclick="api('POST','/api/groups/${g.name}/teardown')">Teardown</button>
        <button class="danger" onclick="api('DELETE','/api/groups/${g.name}').then(refresh)">Delete</button>
      </div>
      <div class="controls" style="margin-top:8px">
        <div class="input-group">
          <label>Rate/tenant</label>
          <input type="number" id="rate-${g.name}" value="${g.config.publish.rate_per_tenant}" min="1">
        </div>
        <div class="input-group">
          <label>Latency ms</label>
          <input type="number" id="lat-${g.name}" value="${g.config.mock_profile.latency_ms}" min="0">
        </div>
        <div class="input-group">
          <label>Error rate</label>
          <input type="number" id="err-${g.name}" value="${g.config.mock_profile.error_rate}" min="0" max="1" step="0.01">
        </div>
        <div class="input-group" style="justify-content:flex-end">
          <label>&nbsp;</label>
          <button onclick="applySettings('${g.name}')">Apply</button>
        </div>
      </div>
    </div>`;
}

function updateCard(g) {
  const card = document.querySelector(`.card[data-group="${g.name}"]`);
  if (!card) return false;

  const m = g.metrics || {};
  const sets = {
    pub_rate: fmt(m.publish_rate_per_sec),
    del_rate: fmt(m.delivery_rate_per_sec),
    in_flight: m.in_flight || 0,
    p50: m.e2e_latency_p50_ms || 0,
    p95: m.e2e_latency_p95_ms || 0,
    missing: m.missing_total || 0,
    pub_total: m.publish_total || 0,
    del_total: m.delivery_total || 0,
    errors: m.publish_errors || 0,
  };
  for (const [key, val] of Object.entries(sets)) {
    const el = card.querySelector(`[data-m="${key}"]`);
    if (el) el.textContent = val;
  }

  // Update tooltips
  const tooltip = `E2E Latency\np50: ${m.e2e_latency_p50_ms||0}ms\np90: ${m.e2e_latency_p90_ms||0}ms\np95: ${m.e2e_latency_p95_ms||0}ms\np99: ${m.e2e_latency_p99_ms||0}ms\nmax: ${m.e2e_latency_max_ms||0}ms\n\nPublish Latency\np50: ${m.publish_latency_p50_ms||0}ms\np90: ${m.publish_latency_p90_ms||0}ms\np95: ${m.publish_latency_p95_ms||0}ms\np99: ${m.publish_latency_p99_ms||0}ms\nmax: ${m.publish_latency_max_ms||0}ms`;
  card.querySelectorAll('.has-tooltip').forEach(el => el.title = tooltip);

  // Update state badge
  const badge = card.querySelector('.card-state');
  if (badge) {
    badge.textContent = g.state;
    badge.className = 'card-state state-' + g.state;
  }

  return true;
}

async function applySettings(name) {
  const rate = parseInt(document.getElementById('rate-' + name).value);
  const latency = parseInt(document.getElementById('lat-' + name).value);
  const errorRate = parseFloat(document.getElementById('err-' + name).value);
  await api('PATCH', '/api/groups/' + name, {
    rate_per_tenant: rate,
    latency_ms: latency,
    error_rate: errorRate,
  });
}

function fmt(n) {
  if (n == null) return '0';
  return n < 10 ? n.toFixed(1) : Math.round(n).toString();
}

async function refresh() {
  const groups = await api('GET', '/api/groups');
  const container = document.getElementById('groups');

  // Track which groups exist for add/remove detection
  const currentNames = new Set(Array.from(container.querySelectorAll('.card')).map(c => c.dataset.group));
  const newNames = new Set(groups.map(g => g.name));

  // Remove deleted groups
  for (const name of currentNames) {
    if (!newNames.has(name)) {
      container.querySelector(`.card[data-group="${name}"]`).remove();
    }
  }

  // Update existing or add new
  for (const g of groups) {
    if (!updateCard(g)) {
      container.insertAdjacentHTML('beforeend', renderCard(g));
    }
  }
}

// Load status
api('GET', '/api/status').then(s => {
  document.getElementById('outpost-url').textContent = 'Outpost: ' + s.outpost_url;
  document.getElementById('mock-url').textContent = 'Mock: ' + s.mock_url;
});

// Poll every 1s
setInterval(refresh, 1000);
refresh();
