(function() {
  "use strict";
  var STATUS_COLORS = {
    RUNNING: "#58a6ff", SPAWNED: "#58a6ff",
    READY: "#3fb950", MERGED: "#3fb950", LANDED: "#3fb950",
    BLOCKED: "#f85149", FAILED: "#f85149", ERROR: "#f85149",
    STALE: "#8b949e", ORPHANED: "#8b949e", IDLE: "#8b949e"
  };
  var DEFAULT_COLOR = "#8b949e";

  function statusColor(s) { return STATUS_COLORS[(s||"").toUpperCase()] || DEFAULT_COLOR; }

  function zone(status) {
    var s = (status||"").toUpperCase();
    if (s === "RUNNING" || s === "SPAWNED") return "working";
    if (s === "BLOCKED" || s === "FAILED" || s === "ERROR" || s === "STALE" || s === "ORPHANED") return "attention";
    if (s === "READY" || s === "MERGED" || s === "LANDED") return "completed";
    return "working";
  }

  function badgeBg(color) { return color + "22"; }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  var startTime = new Date();
  var safetyCache = {};

  function fetchSafety(id) {
    fetch("/api/workspace/" + encodeURIComponent(id) + "/safety")
      .then(function(r) { return r.json(); })
      .then(function(s) { safetyCache[id] = s; updateShield(id, s); })
      .catch(function() {});
  }

  function shieldInfo(s) {
    if (s.bypasses > 0) return { icon: "\uD83D\uDFE0", color: "#f85149", label: "bypass detected" };
    if (s.approvals > 0) return { icon: "\uD83D\uDFE1", color: "#d29922", label: s.approvals + " auto-approval" + (s.approvals > 1 ? "s" : "") };
    return { icon: "\uD83D\uDFE2", color: "#3fb950", label: "fully sandboxed" };
  }

  function updateShield(id, s) {
    var el = document.querySelector('[data-shield="' + id + '"]');
    if (!el) return;
    var info = shieldInfo(s);
    el.style.color = info.color;
    el.innerHTML = info.icon + '<span class="shield-tooltip">' + esc(info.label) +
      (s.approvals ? '<br>approvals: ' + s.approvals : '') +
      (s.bypasses ? '<br>bypasses: ' + s.bypasses : '') + '</span>';
  }

  function renderStats(data) {
    var total = (data || []).length;
    var working = 0, idle = 0, blocked = 0;
    (data || []).forEach(function(ws) {
      var s = (ws.status||"").toUpperCase();
      if (s === "RUNNING" || s === "SPAWNED") working++;
      else if (s === "BLOCKED" || s === "FAILED" || s === "ERROR" || s === "STALE" || s === "ORPHANED") blocked++;
      else idle++;
    });
    var now = new Date();
    var uptimeSec = Math.floor((now - startTime) / 1000);
    var uptimeMin = Math.floor(uptimeSec / 60);
    var uptimeStr = uptimeMin > 0 ? uptimeMin + "m " + (uptimeSec % 60) + "s" : uptimeSec + "s";
    var timeStr = now.toLocaleTimeString();
    var totalApprovals = 0, totalBypasses = 0;
    Object.keys(safetyCache).forEach(function(k) {
      totalApprovals += (safetyCache[k].approvals || 0);
      totalBypasses += (safetyCache[k].bypasses || 0);
    });
    var bypassColor = totalBypasses > 0 ? "#f85149" : "#3fb950";
    var bypassClass = totalBypasses > 0 ? ' stat-pulse' : '';
    var bar = document.getElementById("statsBar");
    bar.innerHTML =
      '<span class="stat-pill" style="color:#c9d1d9;background:#30363d">' + total + ' total</span>' +
      '<span class="stat-pill" style="color:#58a6ff;background:#58a6ff22">' + working + ' working</span>' +
      '<span class="stat-pill" style="color:#8b949e;background:#8b949e22">' + idle + ' idle</span>' +
      '<span class="stat-pill" style="color:#f85149;background:#f8514922">' + blocked + ' blocked</span>' +
      '<span class="stat-pill" style="color:#3fb950;background:#3fb95022">' + totalApprovals + ' approvals</span>' +
      '<span class="stat-pill' + bypassClass + '" style="color:' + bypassColor + ';background:' + bypassColor + '22">' + totalBypasses + ' bypasses</span>' +
      '<span class="stat-meta">uptime ' + esc(uptimeStr) + ' &middot; refreshed ' + esc(timeStr) + '</span>';
  }

  function render(data) {
    renderStats(data);
    var groups = { working: [], attention: [], completed: [] };
    (data || []).forEach(function(ws) { groups[zone(ws.status)].push(ws); });

    var zoneConfig = [
      { key: "working", title: "Working", color: "#58a6ff" },
      { key: "attention", title: "Needs Attention", color: "#f85149" },
      { key: "completed", title: "Completed", color: "#3fb950" }
    ];

    var html = "";
    var hasAny = false;
    zoneConfig.forEach(function(z) {
      var items = groups[z.key];
      if (items.length === 0) return;
      hasAny = true;
      html += '<div class="zone">';
      html += '<div class="zone-title" style="color:' + esc(z.color) + '">' + esc(z.title) +
              ' <span class="count">(' + items.length + ')</span></div>';
      html += '<div class="cards">';
      items.forEach(function(ws) {
        var c = statusColor(ws.status);
        var isActive = ws.id === window.activeTerminalId;
        html += '<div class="card' + (isActive ? ' active' : '') + '" data-id="' + esc(ws.id) + '">';
        html += '<div class="card-top">';
        html += '<span class="card-id">' + esc(ws.id) + '</span>';
        html += '<span class="shield" data-shield="' + esc(ws.id) + '"></span>';
        html += '<span class="badge" style="color:' + esc(c) + ';background:' + badgeBg(c) + '">' + esc(ws.status) + '</span>';
        html += '</div>';
        html += '<div class="card-details">';
        html += '<span><span class="label">task</span> ' + esc(ws.task) + '</span>';
        html += '<span><span class="label">diff</span> ' + esc(ws.diff) + '</span>';
        html += '<span><span class="label">age</span> ' + esc(ws.age) + '</span>';
        html += '</div>';
        var su = (ws.status||"").toUpperCase();
        if (su === "BLOCKED" || su === "RUNNING" || su === "SPAWNED") {
          html += '<div class="card-actions">';
          html += '<button data-approve="' + esc(ws.id) + '">approve</button>';
          html += '<input data-send-input="' + esc(ws.id) + '" placeholder="message\u2026" onclick="event.stopPropagation()">';
          html += '<button data-send="' + esc(ws.id) + '">send</button>';
          html += '</div>';
        }
        html += '</div>';
      });
      html += '</div></div>';
    });
    if (!hasAny) {
      html = '<div class="empty-state">No workspaces found.</div>';
    }
    document.getElementById("sidebar").innerHTML = html;

    document.querySelectorAll(".card").forEach(function(el) {
      el.addEventListener("click", function() {
        var id = el.getAttribute("data-id");
        window.activeTerminalId = id;
        window.openTerminal(id, id);
      });
    });
    document.querySelectorAll("[data-approve]").forEach(function(btn) {
      btn.addEventListener("click", function(e) {
        e.stopPropagation();
        fetch("/api/workspaces/" + encodeURIComponent(btn.getAttribute("data-approve")) + "/approve", {method:"POST"});
      });
    });
    // Fetch safety status for each workspace
    (data || []).forEach(function(ws) { fetchSafety(ws.id); });
    document.querySelectorAll("[data-send]").forEach(function(btn) {
      btn.addEventListener("click", function(e) {
        e.stopPropagation();
        var id = btn.getAttribute("data-send");
        var input = document.querySelector("[data-send-input='" + id + "']");
        if (!input || !input.value) return;
        fetch("/api/workspaces/" + encodeURIComponent(id) + "/send", {
          method:"POST", headers:{"Content-Type":"application/json"},
          body: JSON.stringify({message: input.value})
        });
        input.value = "";
      });
    });
  }


  // Export audit CSV
  document.getElementById("exportAudit").addEventListener("click", function() {
    window.location.href = "/api/audit/export?format=csv&since=168h";
  });

  function poll() {
    fetch("/api/workspaces").then(function(r) { return r.json(); }).then(render).catch(function() {});
    fetch("/api/events").then(function(r) { return r.json(); }).then(function(events) {
      if (typeof renderActivity === "function") renderActivity(events);
    }).catch(function() {});
    setTimeout(poll, 5000);
  }
  poll();
})();
