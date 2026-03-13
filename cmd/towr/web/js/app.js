(function () {
  "use strict";

  var STATUS_COLORS = {
    RUNNING: "#58a6ff", SPAWNED: "#58a6ff",
    READY: "#3fb950", MERGED: "#3fb950", LANDED: "#3fb950",
    BLOCKED: "#f85149", FAILED: "#f85149", ERROR: "#f85149",
    STALE: "#8b949e", ORPHANED: "#8b949e", IDLE: "#8b949e"
  };
  var DEFAULT_COLOR = "#8b949e";
  var PAGE_LOAD = Date.now();
  var lastJSON = "";
  var safetyCache = {};

  function statusColor(s) { return STATUS_COLORS[(s || "").toUpperCase()] || DEFAULT_COLOR; }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  // --- Sorting helpers ---
  function sortPriority(status) {
    var s = (status || "").toUpperCase();
    if (s === "BLOCKED" || s === "FAILED" || s === "ERROR") return 0;
    if (s === "RUNNING" || s === "SPAWNED") return 1;
    if (s === "STALE" || s === "ORPHANED" || s === "IDLE") return 2;
    return 3; // completed
  }

  function isCompleted(status) {
    var s = (status || "").toUpperCase();
    return s === "READY" || s === "MERGED" || s === "LANDED";
  }

  function isBlocked(status) {
    var s = (status || "").toUpperCase();
    return s === "BLOCKED" || s === "FAILED" || s === "ERROR";
  }

  function isWorking(status) {
    var s = (status || "").toUpperCase();
    return s === "RUNNING" || s === "SPAWNED";
  }

  // --- Age parsing & opacity ---
  function parseAgeMinutes(age) {
    if (!age) return 0;
    var m = 0;
    var hMatch = age.match(/(\d+)h/);
    var mMatch = age.match(/(\d+)m/);
    if (hMatch) m += parseInt(hMatch[1], 10) * 60;
    if (mMatch) m += parseInt(mMatch[1], 10);
    return m;
  }

  function completedOpacity(age) {
    var mins = parseAgeMinutes(age);
    if (mins < 30) return 0.7;
    if (mins <= 120) return 0.5;
    return 0.3;
  }

  // --- Uptime ---
  function uptimeStr() {
    var sec = Math.floor((Date.now() - PAGE_LOAD) / 1000);
    var m = Math.floor(sec / 60);
    var s = sec % 60;
    return m > 0 ? m + "m " + s + "s" : s + "s";
  }

  // --- Safety shields ---
  function fetchSafety(id) {
    fetch("/api/workspace/" + encodeURIComponent(id) + "/safety")
      .then(function (r) { return r.json(); })
      .then(function (s) {
        safetyCache[id] = s;
        var el = document.querySelector('[data-shield="' + id + '"]');
        if (el) renderShield(el, s);
      })
      .catch(function () {});
  }

  function renderShield(el, s) {
    if (s.bypasses > 0) {
      el.textContent = "\uD83D\uDEE1\uFE0F bypass";
      el.style.color = "#f85149";
      el.title = "bypass detected — bypasses: " + s.bypasses;
    } else if (s.approvals > 0) {
      el.textContent = "\uD83D\uDEE1\uFE0F " + s.approvals + " approved";
      el.style.color = "#d29922";
      el.title = s.approvals + " auto-approval" + (s.approvals > 1 ? "s" : "");
    } else {
      el.textContent = "\uD83D\uDEE1\uFE0F sandboxed";
      el.style.color = "#3fb950";
      el.title = "fully sandboxed";
    }
  }

  // --- Score cards ---
  function renderCounters(data) {
    var total = data.length;
    var working = 0, blocked = 0, completed = 0;
    var totalApprovals = 0, totalBypasses = 0;

    data.forEach(function (ws) {
      if (isWorking(ws.status)) working++;
      else if (isBlocked(ws.status)) blocked++;
      else if (isCompleted(ws.status)) completed++;
    });
    Object.keys(safetyCache).forEach(function (k) {
      totalApprovals += safetyCache[k].approvals || 0;
      totalBypasses += safetyCache[k].bypasses || 0;
    });

    var bypassColor = totalBypasses > 0 ? "#f85149" : "#3fb950";
    var bypassClass = totalBypasses > 0 ? " stat-pulse" : "";

    var bar = document.getElementById("counters");
    bar.innerHTML =
      '<span class="stat-pill" style="color:#c9d1d9;background:#30363d">' + total + " total</span>" +
      '<span class="stat-pill" style="color:#58a6ff;background:#58a6ff22">' + working + " working</span>" +
      '<span class="stat-pill" style="color:#f85149;background:#f8514922">' + blocked + " blocked</span>" +
      '<span class="stat-pill" style="color:#3fb950;background:#3fb95022">' + completed + " completed</span>" +
      '<span class="stat-pill' + bypassClass + '" style="color:' + bypassColor + ";background:" + bypassColor + '22">' + totalBypasses + " bypasses</span>" +
      '<span class="stat-pill" style="color:#3fb950;background:#3fb95022">' + totalApprovals + " approvals</span>" +
      '<span class="stat-meta">uptime ' + esc(uptimeStr()) + "</span>";
  }

  // --- Workspace list ---
  function renderWorkspaces(data) {
    var sorted = data.slice().sort(function (a, b) {
      return sortPriority(a.status) - sortPriority(b.status);
    });

    if (sorted.length === 0) {
      document.getElementById("workspaceList").innerHTML =
        '<div class="empty-state">No workspaces found.</div>';
      return;
    }

    var html = '<div class="cards">';
    sorted.forEach(function (ws) {
      var c = statusColor(ws.status);
      var opacity = isCompleted(ws.status) ? completedOpacity(ws.age) : 1;
      html += '<div class="card" data-id="' + esc(ws.id) + '" style="border-left:3px solid ' + c +
        ";opacity:" + opacity + '">';
      html += '<div class="card-top">';
      html += '<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:' +
        c + ';margin-right:6px;flex-shrink:0"></span>';
      html += '<span class="card-id">' + esc(ws.id) + "</span>";
      if (ws.agent) {
        html += '<span style="color:#484f58;font-size:0.65rem;margin-left:0.4rem">' + esc(ws.agent) + "</span>";
      }
      html += '<span class="shield" data-shield="' + esc(ws.id) + '"></span>';
      html += '<span class="badge" style="color:' + c + ";background:" + c + '22">' + esc(ws.status) + "</span>";
      html += "</div>";
      html += '<div class="card-details">';
      html += "<span><span class=\"label\">task</span> " + esc(ws.task) + "</span>";
      html += "<span><span class=\"label\">age</span> " + esc(ws.age) + "</span>";
      html += "</div>";
      if (isBlocked(ws.status)) {
        html += '<div class="card-actions">';
        html += '<button data-approve="' + esc(ws.id) + '">Approve</button>';
        html += "</div>";
      }
      html += "</div>";
    });
    html += "</div>";
    document.getElementById("workspaceList").innerHTML = html;

    // Bind events
    document.querySelectorAll(".card").forEach(function (el) {
      el.addEventListener("click", function () {
        var id = el.getAttribute("data-id");
        if (typeof window.expandWorkspace === "function") {
          window.expandWorkspace(id);
        }
      });
    });
    document.querySelectorAll("[data-approve]").forEach(function (btn) {
      btn.addEventListener("click", function (e) {
        e.stopPropagation();
        fetch("/api/workspaces/" + encodeURIComponent(btn.getAttribute("data-approve")) + "/approve",
          { method: "POST" });
      });
    });
  }

  // --- Main render with DOM caching ---
  function render(data) {
    var json = JSON.stringify(data);
    if (json === lastJSON) {
      // Still update uptime even if data unchanged
      var meta = document.querySelector(".stat-meta");
      if (meta) meta.textContent = "uptime " + uptimeStr();
      return;
    }
    lastJSON = json;

    renderCounters(data);
    renderWorkspaces(data);

    // Fetch safety for each workspace
    (data || []).forEach(function (ws) { fetchSafety(ws.id); });
  }

  // --- Export audit CSV ---
  document.getElementById("exportAudit").addEventListener("click", function () {
    window.location.href = "/api/audit/export?format=csv&since=7d";
  });

  // --- Update header meta ---
  var headerMeta = document.querySelector(".header-meta");
  if (headerMeta) {
    headerMeta.innerHTML = '<span class="dot"></span>live &middot; refreshing every 4s';
  }

  // --- Poll loop ---
  function poll() {
    fetch("/api/workspaces")
      .then(function (r) { return r.json(); })
      .then(render)
      .catch(function () {});
    fetch("/api/events")
      .then(function (r) { return r.json(); })
      .then(function (events) {
        if (typeof renderActivity === "function") renderActivity(events);
      })
      .catch(function () {});
    setTimeout(poll, 4000);
  }
  poll();
})();
