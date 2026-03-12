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

  var activeId = null;
  var evtSource = null;

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  var startTime = new Date();

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
    var bar = document.getElementById("statsBar");
    bar.innerHTML =
      '<span class="stat-pill" style="color:#c9d1d9;background:#30363d">' + total + ' total</span>' +
      '<span class="stat-pill" style="color:#58a6ff;background:#58a6ff22">' + working + ' working</span>' +
      '<span class="stat-pill" style="color:#8b949e;background:#8b949e22">' + idle + ' idle</span>' +
      '<span class="stat-pill" style="color:#f85149;background:#f8514922">' + blocked + ' blocked</span>' +
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
        var isActive = ws.id === activeId;
        html += '<div class="card' + (isActive ? ' active' : '') + '" data-id="' + esc(ws.id) + '">';
        html += '<div class="card-top">';
        html += '<span class="card-id">' + esc(ws.id) + '</span>';
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
      el.addEventListener("click", function() { openTerminal(el.getAttribute("data-id")); });
    });
    document.querySelectorAll("[data-approve]").forEach(function(btn) {
      btn.addEventListener("click", function(e) {
        e.stopPropagation();
        fetch("/api/workspaces/" + encodeURIComponent(btn.getAttribute("data-approve")) + "/approve", {method:"POST"});
      });
    });
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

  function openTerminal(id) {
    activeId = id;
    var panel = document.getElementById("termPanel");
    var body = document.getElementById("termBody");
    var title = document.getElementById("termTitle");
    panel.classList.add("open");
    title.textContent = id;
    body.textContent = "connecting...";

    if (evtSource) { evtSource.close(); evtSource = null; }
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    evtSource.onmessage = function(e) {
      body.textContent = "";
      var lines = e.data.split("\n");
      body.textContent = lines.join("\n");
      body.scrollTop = body.scrollHeight;
    };
    evtSource.onerror = function() {
      body.textContent += "\n[stream disconnected]";
    };

    document.querySelectorAll(".card").forEach(function(el) {
      el.classList.toggle("active", el.getAttribute("data-id") === id);
    });
  }

  document.getElementById("termClose").addEventListener("click", function() {
    document.getElementById("termPanel").classList.remove("open");
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    document.querySelectorAll(".card.active").forEach(function(el) { el.classList.remove("active"); });
  });

  var EVENT_COLORS = {
    "task.completed": "#3fb950", "task.failed": "#f85149",
    "task.dispatched": "#58a6ff", "task.blocked": "#d29922"
  };

  document.getElementById("actToggle").addEventListener("click", function() {
    this.classList.toggle("open");
    document.getElementById("actFeed").classList.toggle("open");
  });

  function renderEvents(events) {
    var feed = document.getElementById("actFeed");
    var countEl = document.getElementById("actCount");
    countEl.textContent = "(" + (events||[]).length + " events)";
    var html = "";
    (events||[]).forEach(function(ev) {
      var ts = new Date(ev.ts).toLocaleTimeString();
      var c = EVENT_COLORS[ev.kind] || "#8b949e";
      var summary = "";
      if (ev.data && ev.data.summary) summary = ev.data.summary;
      else if (ev.data && ev.data.message) summary = ev.data.message;
      html += '<div class="evt-row">';
      html += '<span class="evt-ts">' + esc(ts) + '</span>';
      html += '<span class="evt-ws">' + esc(ev.workspace_id||"-") + '</span>';
      html += '<span class="evt-kind" style="color:' + c + '">' + esc(ev.kind) + '</span>';
      html += '<span>' + esc(summary) + '</span>';
      html += '</div>';
    });
    feed.innerHTML = html;
  }

  function poll() {
    fetch("/api/workspaces").then(function(r) { return r.json(); }).then(render).catch(function() {});
    fetch("/api/events").then(function(r) { return r.json(); }).then(renderEvents).catch(function() {});
    setTimeout(poll, 5000);
  }
  poll();
})();
