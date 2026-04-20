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
    <div class="card">
      <div class="card-header">
        <span class="card-name">${g.name}</span>
        <span class="card-state state-${g.state}">${g.state}</span>
      </div>
      <div class="metrics">
        <div class="metric"><div class="metric-value">${fmt(m.publish_rate_per_sec)}</div><div class="metric-label">pub/s</div></div>
        <div class="metric"><div class="metric-value">${fmt(m.delivery_rate_per_sec)}</div><div class="metric-label">del/s</div></div>
        <div class="metric"><div class="metric-value">${m.in_flight || 0}</div><div class="metric-label">in-flight</div></div>
        <div class="metric"><div class="metric-value">${m.e2e_latency_p50_ms || 0}</div><div class="metric-label">p50 ms</div></div>
        <div class="metric"><div class="metric-value">${m.e2e_latency_p95_ms || 0}</div><div class="metric-label">p95 ms</div></div>
        <div class="metric"><div class="metric-value">${m.missing_total || 0}</div><div class="metric-label">missing</div></div>
        <div class="metric"><div class="metric-value">${m.publish_total || 0}</div><div class="metric-label">pub total</div></div>
        <div class="metric"><div class="metric-value">${m.delivery_total || 0}</div><div class="metric-label">del total</div></div>
        <div class="metric"><div class="metric-value">${m.publish_errors || 0}</div><div class="metric-label">errors</div></div>
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
        <div class="slider-group">
          <label>Rate/tenant: <span id="rate-${g.name}">${g.config.publish.rate_per_tenant}</span></label>
          <input type="range" min="1" max="5000" value="${g.config.publish.rate_per_tenant}"
            oninput="document.getElementById('rate-${g.name}').textContent=this.value"
            onchange="api('PATCH','/api/groups/${g.name}',{rate_per_tenant:parseInt(this.value)})">
        </div>
        <div class="slider-group">
          <label>Latency ms: <span id="lat-${g.name}">${g.config.mock_profile.latency_ms}</span></label>
          <input type="range" min="0" max="5000" value="${g.config.mock_profile.latency_ms}"
            oninput="document.getElementById('lat-${g.name}').textContent=this.value"
            onchange="api('PATCH','/api/groups/${g.name}',{latency_ms:parseInt(this.value)})">
        </div>
        <div class="slider-group">
          <label>Error rate: <span id="err-${g.name}">${g.config.mock_profile.error_rate}</span></label>
          <input type="range" min="0" max="100" value="${g.config.mock_profile.error_rate * 100}"
            oninput="document.getElementById('err-${g.name}').textContent=(this.value/100).toFixed(2)"
            onchange="api('PATCH','/api/groups/${g.name}',{error_rate:parseInt(this.value)/100})">
        </div>
      </div>
    </div>`;
}

function fmt(n) {
  if (n == null) return '0';
  return n < 10 ? n.toFixed(1) : Math.round(n).toString();
}

async function refresh() {
  const groups = await api('GET', '/api/groups');
  document.getElementById('groups').innerHTML = groups.map(renderCard).join('');
}

// Load status
api('GET', '/api/status').then(s => {
  document.getElementById('outpost-url').textContent = 'Outpost: ' + s.outpost_url;
  document.getElementById('mock-url').textContent = 'Mock: ' + s.mock_url;
});

// Poll every 1s
setInterval(refresh, 1000);
refresh();
