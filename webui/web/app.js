(function () {
  'use strict';

  var params = new URLSearchParams(window.location.search);
  var TOKEN = params.get('token') || '';

  var nodes = new Map();
  var edges = new Map();
  var runs = new Map();
  var selectedRunID = null;
  var selectedNodeID = null;
  var activeView = 'graph';

  function applyDelta(ev) {
    switch (ev.kind) {
      case 'node_upsert':
        if (ev.node) {
          var existing = nodes.get(ev.node.id);
          if (existing && existing.rev >= ev.rev) return;
          nodes.set(ev.node.id, ev.node);
          ensureRun(ev.node.run_id, ev.node.status);
        }
        break;
      case 'node_status':
        if (ev.node) {
          var n = nodes.get(ev.node.id);
          if (!n) {
            nodes.set(ev.node.id, ev.node);
          } else if (n.rev < ev.rev) {
            n.status = ev.node.status;
            n.rev = ev.rev;
            n.t_end = ev.node.t_end;
            n.duration_ms = ev.node.duration_ms;
          }
          ensureRun(ev.node.run_id, ev.node.status);
        }
        break;
      case 'node_merge':
        if (ev.node && ev.old_id) {
          nodes.delete(ev.old_id);
          nodes.set(ev.node.id, ev.node);
        }
        break;
      case 'edge_upsert':
        if (ev.edge) {
          var ee = edges.get(ev.edge.id);
          if (ee && ee.rev >= ev.rev) return;
          edges.set(ev.edge.id, ev.edge);
        }
        break;
      case 'edge_delete':
        if (ev.edge) edges.delete(ev.edge.id);
        break;
      case 'run_started':
        ensureRun(ev.run_id, 'running');
        break;
      case 'run_ended':
        var r = runs.get(ev.run_id);
        if (r) r.status = ev.node ? (ev.node.status || 'ok') : 'ok';
        break;
      case 'session_ended':
        var rs = runs.get(ev.run_id);
        if (rs) rs.status = 'ok';
        break;
      default:
        break;
    }
    scheduleRender();
  }

  function ensureRun(runID, status) {
    if (!runID) return;
    if (!runs.has(runID)) {
      runs.set(runID, { id: runID, status: status || 'pending' });
    } else if (status === 'running') {
      runs.get(runID).status = 'running';
    }
  }

  var renderPending = false;

  function scheduleRender() {
    if (renderPending) return;
    renderPending = true;
    requestAnimationFrame(function () {
      renderPending = false;
      render();
    });
  }

  function bfsLayout() {
    var nodeList = Array.from(nodes.values());
    if (selectedRunID) {
      nodeList = nodeList.filter(function (n) { return n.run_id === selectedRunID; });
    }

    var edgeList = Array.from(edges.values());
    var childSet = new Set();
    edgeList.forEach(function (e) {
      if (e.type === 'parent_child') childSet.add(e.dst);
    });

    var roots = nodeList.filter(function (n) { return !childSet.has(n.id); });
    if (roots.length === 0 && nodeList.length > 0) roots = [nodeList[0]];

    var layers = [];
    var visited = new Set();

    function bfs(root) {
      var queue = [{ node: root, depth: 0 }];
      while (queue.length) {
        var item = queue.shift();
        var nd = item.node;
        var depth = item.depth;
        if (visited.has(nd.id)) continue;
        visited.add(nd.id);
        while (layers.length <= depth) layers.push([]);
        layers[depth].push(nd);
        edgeList
          .filter(function (e) { return e.type === 'parent_child' && e.src === nd.id; })
          .forEach(function (e) {
            var child = nodes.get(e.dst);
            if (child) queue.push({ node: child, depth: depth + 1 });
          });
      }
    }

    roots.forEach(bfs);

    nodeList.forEach(function (n) {
      if (!visited.has(n.id)) {
        if (layers.length === 0) layers.push([]);
        layers[layers.length - 1].push(n);
        visited.add(n.id);
      }
    });

    var W = 140, H = 46, PX = 60, PY = 40;
    var positions = new Map();
    layers.forEach(function (layer, col) {
      layer.forEach(function (nd, row) {
        positions.set(nd.id, { x: PX + col * (W + PX), y: PY + row * (H + PY) });
      });
    });
    return { positions: positions, nodeW: W, nodeH: H };
  }

  function escapeXML(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function escapeHTML(s) { return escapeXML(s); }

  function renderGraph() {
    var host = document.getElementById('view-graph');
    if (!host) return;

    var layout = bfsLayout();
    var positions = layout.positions;
    var W = layout.nodeW, H = layout.nodeH;

    var nodeList = Array.from(nodes.values());
    if (selectedRunID) {
      nodeList = nodeList.filter(function (n) { return n.run_id === selectedRunID; });
    }

    var edgeList = Array.from(edges.values()).filter(function (e) {
      return positions.has(e.src) && positions.has(e.dst);
    });

    if (nodeList.length === 0) {
      host.innerHTML = '<p class="hint" style="padding:20px">No nodes yet — waiting for events…</p>';
      return;
    }

    var maxX = 0, maxY = 0;
    positions.forEach(function (p) {
      var rx = p.x + W + 60;
      var ry = p.y + H + 40;
      if (rx > maxX) maxX = rx;
      if (ry > maxY) maxY = ry;
    });
    var svgW = Math.max(maxX, 400);
    var svgH = Math.max(maxY, 300);

    var svgNS = 'http://www.w3.org/2000/svg';
    var parts = ['<svg xmlns="' + svgNS + '" width="' + svgW + '" height="' + svgH + '">'];

    edgeList.forEach(function (e) {
      var s = positions.get(e.src), d = positions.get(e.dst);
      if (!s || !d) return;
      var x1 = s.x + W, y1 = s.y + H / 2, x2 = d.x, y2 = d.y + H / 2;
      var mx = (x1 + x2) / 2;
      parts.push('<path class="edge-line" d="M' + x1 + ',' + y1 +
        ' C' + mx + ',' + y1 + ' ' + mx + ',' + y2 + ' ' + x2 + ',' + y2 + '"/>');
    });

    nodeList.forEach(function (nd) {
      var p = positions.get(nd.id);
      if (!p) return;
      var typeClass = 'node-' + escapeXML(nd.type || 'marker');
      var statusClass = 'status-' + escapeXML(nd.status || 'pending');
      var selExtra = nd.id === selectedNodeID ? ' stroke-width="3"' : '';
      var label = (nd.name || nd.type || nd.id || '').slice(0, 18);
      parts.push('<g class="node-group" data-id="' + escapeXML(nd.id) + '">');
      parts.push('<rect class="node-rect ' + typeClass + ' ' + statusClass + '"' +
        ' x="' + p.x + '" y="' + p.y + '" width="' + W + '" height="' + H + '"' + selExtra + '/>');
      parts.push('<text class="node-label" x="' + (p.x + 6) + '" y="' + (p.y + 16) + '">' +
        escapeXML(label) + '</text>');
      parts.push('<text class="node-label" style="fill:#8b949e;font-size:9px" x="' + (p.x + 6) + '" y="' + (p.y + 30) + '">' +
        escapeXML(nd.status || '') + '</text>');
      parts.push('</g>');
    });

    parts.push('</svg>');

    host.innerHTML = parts.join('');

    host.querySelectorAll('.node-group').forEach(function (g) {
      g.addEventListener('click', function () {
        selectedNodeID = g.getAttribute('data-id');
        if (activeView === 'inspector') {
          renderInspector(nodes.get(selectedNodeID));
        }
        scheduleRender();
      });
    });
  }

  function renderTimeline() {
    var host = document.getElementById('view-timeline');
    if (!host) return;

    var nodeList = Array.from(nodes.values()).filter(function (n) {
      return n.t_start && (selectedRunID ? n.run_id === selectedRunID : true);
    });

    if (nodeList.length === 0) {
      host.innerHTML = '<p class="hint" style="padding:20px">No timing data yet</p>';
      return;
    }

    var now = Date.now();
    var minT = Infinity, maxT = -Infinity;
    nodeList.forEach(function (n) {
      var s = new Date(n.t_start).getTime();
      var e = n.t_end ? new Date(n.t_end).getTime() : now;
      if (s < minT) minT = s;
      if (e > maxT) maxT = e;
    });

    var totalMs = maxT - minT || 1;
    var BAR_W = 800, rowH = 24, PY = 30, PX = 160;
    var viewW = BAR_W + PX + 20;
    var viewH = PY + nodeList.length * (rowH + 4) + 20;

    var svgNS = 'http://www.w3.org/2000/svg';
    var parts = ['<svg xmlns="' + svgNS + '" width="' + viewW + '" height="' + viewH + '">'];

    nodeList.forEach(function (n, i) {
      var s = new Date(n.t_start).getTime();
      var e = n.t_end ? new Date(n.t_end).getTime() : now;
      var x = PX + ((s - minT) / totalMs) * BAR_W;
      var w = Math.max(((e - s) / totalMs) * BAR_W, 4);
      var y = PY + i * (rowH + 4);
      var typeClass = 'node-' + escapeXML(n.type || 'marker');
      var label = (n.name || n.id || '').slice(0, 22);
      parts.push('<rect class="timeline-bar ' + typeClass + '"' +
        ' x="' + x + '" y="' + y + '" width="' + w + '" height="' + rowH + '"/>');
      parts.push('<text class="timeline-label" x="4" y="' + (y + 16) + '">' +
        escapeXML(label) + '</text>');
    });

    parts.push('</svg>');
    host.innerHTML = parts.join('');
  }

  function renderInspector(nd) {
    var panel = document.getElementById('inspector-panel');
    if (!panel) return;
    if (!nd) {
      panel.innerHTML = '<p class="hint">Click a node in the Graph view</p>';
      return;
    }
    var rows = [
      ['id', nd.id],
      ['type', nd.type],
      ['name', nd.name || ''],
      ['status', nd.status || ''],
      ['run_id', nd.run_id || ''],
      ['t_start', nd.t_start || ''],
      ['t_end', nd.t_end || ''],
      ['duration_ms', nd.duration_ms != null ? String(nd.duration_ms) : ''],
      ['tokens_in', nd.tokens_in != null ? String(nd.tokens_in) : ''],
      ['tokens_out', nd.tokens_out != null ? String(nd.tokens_out) : ''],
      ['cost_usd', typeof nd.cost_usd === 'number' ? nd.cost_usd.toFixed(6) : (nd.cost_usd != null ? String(nd.cost_usd) : '')],
      ['payload_hash', nd.payload_hash || ''],
      ['tier', nd.tier || ''],
    ];

    if (nd.attrs && typeof nd.attrs === 'object') {
      Object.keys(nd.attrs).forEach(function (k) {
        rows.push(['attr:' + k, JSON.stringify(nd.attrs[k])]);
      });
    }

    if (Array.isArray(nd.sources)) {
      nd.sources.forEach(function (s, i) {
        var val = (s && s.source ? s.source : '') + (s && s.obs_id ? ' / ' + s.obs_id : '');
        rows.push(['source[' + i + ']', val]);
      });
    }

    var html = '<h3>' + escapeHTML(nd.name || nd.type || nd.id || '') + '</h3><table>';
    rows.forEach(function (row) {
      if (row[1] === '' || row[1] == null) return;
      html += '<tr><td>' + escapeHTML(row[0]) + '</td><td>' + escapeHTML(String(row[1])) + '</td></tr>';
    });
    html += '</table>';
    panel.innerHTML = html;
  }

  function renderRuns() {
    var list = document.getElementById('runs-list');
    if (!list) return;
    var filterEl = document.getElementById('runs-filter');
    var filterVal = filterEl ? filterEl.value : '';
    var runArr = Array.from(runs.values()).filter(function (r) {
      return !filterVal || r.id.indexOf(filterVal) !== -1;
    });
    var html = '';
    runArr.forEach(function (r) {
      var sel = r.id === selectedRunID ? ' selected' : '';
      var sc = 'run-status-' + escapeXML(r.status || 'pending');
      html += '<li data-run="' + escapeXML(r.id) + '" class="' + sel + '">' +
        '<span class="run-status ' + sc + '"></span>' +
        escapeHTML(r.id.slice(0, 32)) + '</li>';
    });
    list.innerHTML = html;
    list.querySelectorAll('li').forEach(function (li) {
      li.addEventListener('click', function () {
        selectedRunID = li.getAttribute('data-run');
        scheduleRender();
      });
    });
  }

  function render() {
    if (activeView === 'graph') renderGraph();
    else if (activeView === 'timeline') renderTimeline();
    else if (activeView === 'inspector') renderInspector(selectedNodeID ? nodes.get(selectedNodeID) : null);
    else if (activeView === 'runs') renderRuns();
  }

  document.querySelectorAll('.tab').forEach(function (btn) {
    btn.addEventListener('click', function () {
      document.querySelectorAll('.tab').forEach(function (b) { b.classList.remove('active'); });
      btn.classList.add('active');
      activeView = btn.getAttribute('data-view');
      var views = document.querySelectorAll('.view');
      views.forEach(function (v) { v.classList.remove('active'); });
      var view = document.getElementById('view-' + activeView);
      if (view) view.classList.add('active');
      render();
    });
  });

  var filterInput = document.getElementById('runs-filter');
  if (filterInput) {
    filterInput.addEventListener('input', function () { renderRuns(); });
  }

  var connStatus = document.getElementById('conn-status');

  function connect() {
    var es = new EventSource('/v1/subscribe?token=' + encodeURIComponent(TOKEN));
    es.onopen = function () {
      if (connStatus) {
        connStatus.textContent = 'connected';
        connStatus.className = 'connected';
      }
    };
    es.onerror = function () {
      if (connStatus) {
        connStatus.textContent = 'reconnecting…';
        connStatus.className = 'error';
      }
    };
    es.onmessage = function (e) {
      try {
        var ev = JSON.parse(e.data);
        applyDelta(ev);
      } catch (_) {}
    };
  }

  connect();
  render();
}());
