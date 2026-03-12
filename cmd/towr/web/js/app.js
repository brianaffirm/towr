(function() {
  "use strict";

  // --- constants ---
  var POLL_MS = 4000;
  var STATUS_COLORS = {
    RUNNING: "#58a6ff", SPAWNED: "#58a6ff",
    READY: "#3fb950", MERGED: "#3fb950", LANDED: "#3fb950",
    BLOCKED: "#f85149", FAILED: "#f85149", ERROR: "#f85149",
    STALE: "#8b949e", ORPHANED: "#8b949e", IDLE: "#8b949e"
  };
  var DEFAULT_COLOR = "#8b949e";
  var EVENT_COLORS = {
    "task.completed": "#3fb950", "task.failed": "#f85149",
    "task.dispatched": "#58a6ff", "task.blocked": "#d29922"
  };
  var ZONES = [
    { key: "attention", title: "Needs Attention", color: "#f85149" },
    { key: "working",   title: "Working",         color: "#58a6ff" },
    { key: "completed", title: "Completed",       color: "#3fb950" }
  ];

  // --- state ---
  var activeId = null;
  var evtSource = null;
  var zoneOpen = { attention: true, working: true, completed: true };

  // --- DOM refs ---
  var $statsBar  = document.getElementById("statsBar");
  var $sidebar   = document.getElementById("sidebar");
  var $termPanel = document.getElementById("termPanel");
  var $termBody  = document.getElementById("termBody");
  var $termTitle = document.getElementById("termTitle");
  var $termClose = document.getElementById("termClose");
  var $actToggle = document.getElementById("actToggle");
  var $actFeed   = document.getElementById("actFeed");
  var $actCount  = document.getElementById("actCount");
  var $dot       = document.querySelector(".header-meta .dot");

  // --- helpers ---
  function statusColor(s) { return STATUS_COLORS[(s || "").toUpperCase()] || DEFAULT_COLOR; }

  function zoneKey(status) {
    var s = (status || "").toUpperCase();
    if (s === "BLOCKED" || s === "FAILED" || s === "ERROR" || s === "STALE" || s === "ORPHANED") return "attention";
    if (s === "RUNNING" || s === "SPAWNED") return "working";
    if (s === "READY" || s === "MERGED" || s === "LANDED" || s === "IDLE") return "completed";
    return "working";
  }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s || "";
    return d.innerHTML;
  }

  function relativeTime(iso) {
    if (!iso) return "-";
    var diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
    if (diff < 0) return "just now";
    if (diff < 60) return diff + "s ago";
    var m = Math.floor(diff / 60);
    if (m < 60) return m + "m ago";
    var h = Math.floor(m / 60);
    if (h < 24) return h + "h ago";
    var d = Math.floor(h / 24);
    return d + "d ago";
  }

  function diffStr(ws) {
    var add = ws.lines_added || ws.additions || 0;
    var del = ws.lines_removed || ws.deletions || 0;
    if (!add && !del) return ws.diff || "-";
    return "+" + add + "/-" + del + " lines";
  }

  function pulseDot() {
    if (!$dot) return;
    $dot.style.opacity = "1";
    $dot.style.boxShadow = "0 0 6px #3fb950";
    setTimeout(function() {
      $dot.style.opacity = "";
      $dot.style.boxShadow = "";
    }, 600);
  }

  // --- stats bar ---
  function renderStats(data) {
    var total = data.length;
    var working = 0, blocked = 0, completed = 0;
    data.forEach(function(ws) {
      var z = zoneKey(ws.status);
      if (z === "working") working++;
      else if (z === "attention") blocked++;
      else completed++;
    });
    var pills = [
      { label: total + " total",         c: "#c9d1d9", bg: "#30363d" },
      { label: working + " working",     c: "#58a6ff", bg: "#58a6ff22" },
      { label: blocked + " blocked",     c: "#f85149", bg: "#f8514922" },
      { label: completed + " completed", c: "#3fb950", bg: "#3fb95022" }
    ];
    var html = "";
    pills.forEach(function(p) {
      html += '<span class="stat-pill" style="color:' + p.c + ';background:' + p.bg + '">' + esc(p.label) + '</span>';
    });
    html += '<span class="stat-meta">refreshed ' + esc(new Date().toLocaleTimeString()) + '</span>';
    $statsBar.innerHTML = html;
  }

  // --- zone scaffolding ---
  function ensureZones() {
    if ($sidebar.querySelector(".zone")) return;
    ZONES.forEach(function(z) {
      var div = document.createElement("div");
      div.className = "zone";
      div.id = "zone-" + z.key;
      div.style.display = "none";
      div.innerHTML =
        '<div class="zone-title" style="color:' + z.color + ';cursor:pointer" data-zone-toggle="' + z.key + '">' +
          '<span class="arrow" style="display:inline-block;margin-right:6px;transition:transform 0.15s">&#9660;</span>' +
          esc(z.title) + ' <span class="count"></span>' +
        '</div>' +
        '<div class="cards" data-zone-cards="' + z.key + '"></div>';
      $sidebar.appendChild(div);
    });
    // zone collapse toggles
    $sidebar.addEventListener("click", function(e) {
      var toggle = e.target.closest("[data-zone-toggle]");
      if (!toggle) return;
      var key = toggle.getAttribute("data-zone-toggle");
      zoneOpen[key] = !zoneOpen[key];
      var cards = $sidebar.querySelector("[data-zone-cards='" + key + "']");
      var arrow = toggle.querySelector(".arrow");
      if (zoneOpen[key]) {
        cards.style.display = "";
        arrow.style.transform = "";
      } else {
        cards.style.display = "none";
        arrow.style.transform = "rotate(-90deg)";
      }
    });
  }

  // --- card rendering (in-place updates) ---
  function cardHTML(ws) {
    var c = statusColor(ws.status);
    var bg = c + "22";
    var s = (ws.status || "").toUpperCase();
    var html = '<div class="card-top">';
    html += '<span class="card-id">' + esc(ws.id) + '</span>';
    html += '<span class="badge" style="color:' + esc(c) + ';background:' + bg + '">' + esc(ws.status) + '</span>';
    html += '</div>';
    html += '<div class="card-details">';
    html += '<span><span class="label">task</span> ' + esc(ws.task || ws.task_id || "-") + '</span>';
    if (ws.dispatch_status) {
      html += '<span><span class="label">dispatch</span> ' + esc(ws.dispatch_status) + '</span>';
    }
    html += '<span><span class="label">diff</span> ' + esc(diffStr(ws)) + '</span>';
    var age = ws.age || relativeTime(ws.created_at || ws.started_at);
    html += '<span><span class="label">age</span> ' + esc(age) + '</span>';
    html += '</div>';
    if (s === "BLOCKED" || s === "RUNNING" || s === "SPAWNED") {
      html += '<div class="card-actions">';
      html += '<button data-approve="' + esc(ws.id) + '">approve</button>';
      html += '<input data-send-input="' + esc(ws.id) + '" placeholder="message\u2026" onclick="event.stopPropagation()">';
      html += '<button data-send="' + esc(ws.id) + '">send</button>';
      html += '</div>';
    }
    return html;
  }

  function renderWorkspaces(data) {
    ensureZones();
    renderStats(data);

    var groups = { working: [], attention: [], completed: [] };
    data.forEach(function(ws) { groups[zoneKey(ws.status)].push(ws); });

    ZONES.forEach(function(z) {
      var items = groups[z.key];
      var zoneEl = document.getElementById("zone-" + z.key);
      zoneEl.style.display = items.length ? "" : "none";
      zoneEl.querySelector(".count").textContent = "(" + items.length + ")";

      var container = zoneEl.querySelector("[data-zone-cards]");
      var existing = {};
      container.querySelectorAll(".card").forEach(function(el) {
        existing[el.getAttribute("data-id")] = el;
      });

      var seen = {};
      items.forEach(function(ws, i) {
        seen[ws.id] = true;
        var el = existing[ws.id];
        if (el) {
          // update in-place
          el.innerHTML = cardHTML(ws);
          el.classList.toggle("active", ws.id === activeId);
          // reorder if needed
          if (container.children[i] !== el) container.insertBefore(el, container.children[i]);
        } else {
          // new card
          el = document.createElement("div");
          el.className = "card";
          el.setAttribute("data-id", ws.id);
          el.innerHTML = cardHTML(ws);
          el.style.opacity = "0";
          el.style.transition = "opacity 0.3s ease";
          if (ws.id === activeId) el.classList.add("active");
          if (container.children[i]) {
            container.insertBefore(el, container.children[i]);
          } else {
            container.appendChild(el);
          }
          // trigger fade-in
          requestAnimationFrame(function() { el.style.opacity = "1"; });
        }
      });

      // remove stale cards with fade-out
      Object.keys(existing).forEach(function(id) {
        if (!seen[id]) {
          var el = existing[id];
          el.style.opacity = "0";
          setTimeout(function() { if (el.parentNode) el.parentNode.removeChild(el); }, 300);
        }
      });
    });

    // show empty state if no workspaces
    var emptyEl = $sidebar.querySelector(".empty-state");
    if (data.length === 0) {
      if (!emptyEl) {
        emptyEl = document.createElement("div");
        emptyEl.className = "empty-state";
        emptyEl.textContent = "No workspaces found.";
        $sidebar.appendChild(emptyEl);
      }
    } else if (emptyEl) {
      emptyEl.parentNode.removeChild(emptyEl);
    }
  }

  // --- event delegation for card clicks & actions ---
  $sidebar.addEventListener("click", function(e) {
    var approveBtn = e.target.closest("[data-approve]");
    if (approveBtn) {
      e.stopPropagation();
      fetch("/api/workspaces/" + encodeURIComponent(approveBtn.getAttribute("data-approve")) + "/approve", { method: "POST" });
      return;
    }
    var sendBtn = e.target.closest("[data-send]");
    if (sendBtn) {
      e.stopPropagation();
      var id = sendBtn.getAttribute("data-send");
      var input = $sidebar.querySelector("[data-send-input='" + id + "']");
      if (input && input.value) {
        fetch("/api/workspaces/" + encodeURIComponent(id) + "/send", {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ message: input.value })
        });
        input.value = "";
      }
      return;
    }
    var card = e.target.closest(".card");
    if (card) openTerminal(card.getAttribute("data-id"));
  });

  // --- terminal panel ---
  function openTerminal(id) {
    activeId = id;
    $termPanel.classList.add("open");
    $termTitle.textContent = id;
    $termBody.textContent = "connecting...";

    if (evtSource) { evtSource.close(); evtSource = null; }
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    evtSource.onmessage = function(e) {
      $termBody.textContent = e.data.split("\n").join("\n");
      $termBody.scrollTop = $termBody.scrollHeight;
    };
    evtSource.onerror = function() {
      $termBody.textContent += "\n[stream disconnected]";
    };

    $sidebar.querySelectorAll(".card").forEach(function(el) {
      el.classList.toggle("active", el.getAttribute("data-id") === id);
    });
  }

  $termClose.addEventListener("click", function() {
    $termPanel.classList.remove("open");
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    $sidebar.querySelectorAll(".card.active").forEach(function(el) { el.classList.remove("active"); });
  });

  // --- activity log ---
  $actToggle.addEventListener("click", function() {
    this.classList.toggle("open");
    $actFeed.classList.toggle("open");
  });

  function renderEvents(events) {
    $actCount.textContent = "(" + (events || []).length + " events)";
    var html = "";
    (events || []).forEach(function(ev) {
      var ts = new Date(ev.ts).toLocaleTimeString();
      var c = EVENT_COLORS[ev.kind] || "#8b949e";
      var summary = (ev.data && ev.data.summary) || (ev.data && ev.data.message) || "";
      html += '<div class="evt-row">';
      html += '<span class="evt-ts">' + esc(ts) + '</span>';
      html += '<span class="evt-ws">' + esc(ev.workspace_id || "-") + '</span>';
      html += '<span class="evt-kind" style="color:' + c + '">' + esc(ev.kind) + '</span>';
      html += '<span>' + esc(summary) + '</span>';
      html += '</div>';
    });
    $actFeed.innerHTML = html;
  }

  // --- polling ---
  async function poll() {
    var ok = false;
    try {
      var wsRes = await fetch("/api/workspaces");
      var workspaces = await wsRes.json();
      renderWorkspaces(workspaces || []);
      ok = true;
    } catch (_) { /* workspace fetch failed, skip render */ }
    try {
      var evRes = await fetch("/api/events");
      var events = await evRes.json();
      renderEvents(events || []);
      ok = true;
    } catch (_) { /* events fetch failed, skip render */ }
    if (ok) pulseDot();
    setTimeout(poll, POLL_MS);
  }
  poll();
})();
