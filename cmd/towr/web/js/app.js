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
  var costCache = { tasks: [] };
  var MODEL_COLORS = {
      haiku: "#58a6ff",
      sonnet: "#a78bfa",
      opus: "#f59e0b",
      "codex-mini": "#10b981",
      "gpt-5.3-codex": "#10b981",
      "gpt-5.4": "#10b981",
      "cursor-auto": "#06b6d4",
      "cursor-sonnet": "#06b6d4"
  };

  function statusColor(s) { return STATUS_COLORS[(s || "").toUpperCase()] || DEFAULT_COLOR; }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  function sortPriority(status) {
    var s = (status || "").toUpperCase();
    if (s === "BLOCKED" || s === "FAILED" || s === "ERROR") return 0;
    if (s === "RUNNING" || s === "SPAWNED") return 1;
    if (s === "STALE" || s === "ORPHANED" || s === "IDLE") return 2;
    return 3;
  }

  function isCompleted(s) { var u = (s || "").toUpperCase(); return u === "READY" || u === "MERGED" || u === "LANDED"; }
  function isBlocked(s) { var u = (s || "").toUpperCase(); return u === "BLOCKED" || u === "FAILED" || u === "ERROR"; }
  function isWorking(s) { var u = (s || "").toUpperCase(); return u === "RUNNING" || u === "SPAWNED"; }

  function parseAgeMinutes(age) {
    if (!age) return 0;
    var m = 0;
    var hMatch = age.match(/(\d+)h/);
    var mMatch = age.match(/(\d+)m/);
    if (hMatch) m += parseInt(hMatch[1], 10) * 60;
    if (mMatch) m += parseInt(mMatch[1], 10);
    return m;
  }

  function completedClass(age) {
    var mins = parseAgeMinutes(age);
    if (mins <= 30) return "completed";
    if (mins <= 120) return "completed aged-1";
    return "completed aged-2";
  }

  function uptimeStr() {
    var sec = Math.floor((Date.now() - PAGE_LOAD) / 1000);
    var m = Math.floor(sec / 60);
    var s = sec % 60;
    return m > 0 ? m + "m " + s + "s" : s + "s";
  }

  // --- Safety ---
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
    var icon = "\uD83D\uDEE1\uFE0F";
    if (s.bypasses > 0) {
      el.innerHTML = icon + ' <span style="color:#f85149;font-size:0.7rem">bypass</span>';
      el.title = "bypasses: " + s.bypasses + "\nagent: " + (s.agent || "-") + "\nsandbox: " + (s.sandbox || "-");
    } else if (s.approvals > 0) {
      el.innerHTML = icon + ' <span style="color:#d29922;font-size:0.7rem">' + s.approvals + " approved</span>";
      el.title = "approvals: " + s.approvals + "\nagent: " + (s.agent || "-") + "\nsandbox: " + (s.sandbox || "-");
    } else {
      el.innerHTML = icon + ' <span style="color:#3fb950;font-size:0.7rem">sandboxed</span>';
      el.title = "fully sandboxed\nagent: " + (s.agent || "-") + "\nsandbox: " + (s.sandbox || "-");
    }
  }

  function buildCostLookup(costData) {
      var lookup = {};
      if (costData && costData.tasks) {
          costData.tasks.forEach(function (t) {
              lookup[t.id] = t;
          });
      }
      return lookup;
  }

  // --- Counters ---
  function renderCounters(data) {
    var total = data.length, working = 0, blocked = 0, completed = 0;
    var totalApprovals = 0, totalBypasses = 0;
    data.forEach(function (ws) {
      if (isWorking(ws.status)) working++;
      else if (isBlocked(ws.status)) blocked++;
      else if (isCompleted(ws.status)) completed++;
      else completed++; // IDLE counts as completed
    });
    Object.keys(safetyCache).forEach(function (k) {
      totalApprovals += safetyCache[k].approvals || 0;
      totalBypasses += safetyCache[k].bypasses || 0;
    });

    var items = [
      { value: total, label: "total", color: "var(--text-primary)" },
      { value: working, label: "working", color: "var(--accent-blue)" },
      { value: blocked, label: "blocked", color: "var(--accent-red)" },
      { value: completed, label: "completed", color: "var(--accent-green)" },
      { value: totalBypasses, label: "bypasses", color: totalBypasses > 0 ? "var(--accent-red)" : "var(--accent-green)" },
      { value: totalApprovals, label: "approvals", color: "var(--text-primary)" }
    ];

    var html = "";
    items.forEach(function (item) {
      html += '<div class="counter">';
      html += '<div class="counter-value" style="color:' + item.color + '">' + item.value + '</div>';
      html += '<div class="counter-label">' + item.label + '</div>';
      html += '</div>';
    });
    html += '<div class="counter"><div class="counter-value" style="font-size:14px;color:var(--text-secondary)">' + uptimeStr() + '</div><div class="counter-label">uptime</div></div>';

    document.getElementById("counters").innerHTML = html;
  }

  function renderCostPanel(data) {
      var panel = document.getElementById("costPanel");
      if (!data || data.totalSpent === 0) {
          panel.classList.remove("visible");
          return;
      }
      panel.classList.add("visible");

      var html = '<div class="cost-panel-header">';
      html += '<span class="cost-panel-title">Cost Intelligence</span>';
      html += '<span class="cost-panel-savings">' + Math.round(data.savingsPercent) + '% saved vs opus</span>';
      html += '</div>';
      html += '<div class="cost-panel-body">';
      html += '<span><span class="label">Estimated:</span><span class="value">~$' + data.totalEstimated.toFixed(2) + '</span></span>';
      html += '<span><span class="label">Actual:</span><span class="value green">$' + data.totalSpent.toFixed(2) + '</span></span>';
      // Show diff: over or under estimate
      var diff = data.totalSpent - data.totalEstimated;
      if (Math.abs(diff) >= 0.01) {
          var diffSign = diff > 0 ? "+" : "";
          var diffColor = diff > 0 ? "var(--accent-yellow)" : "var(--accent-green)";
          html += '<span><span class="label">Diff:</span><span class="value" style="color:' + diffColor + '">' + diffSign + diff.toFixed(2) + '</span></span>';
      }
      html += '<span><span class="label">All-opus:</span><span class="value muted">$' + data.totalOpus.toFixed(2) + '</span></span>';
      html += '<span><span class="label">Saved:</span><span class="value green">$' + data.totalSaved.toFixed(2) + '</span></span>';
      html += '</div>';

      panel.innerHTML = html;
  }

  // --- Workspace list ---
  function renderWorkspaces(data) {
    var costLookup = buildCostLookup(costCache);
    var sorted = data.slice().sort(function (a, b) {
      return sortPriority(a.status) - sortPriority(b.status);
    });

    if (sorted.length === 0) {
      document.getElementById("workspaceList").innerHTML =
        '<div style="text-align:center;padding:60px 20px;color:var(--text-muted);">No workspaces running. Start with: towr orchestrate plan.yaml</div>';
      return;
    }

    var html = "";
    sorted.forEach(function (ws) {
      var c = statusColor(ws.status);
      var cls = "ws-row";
      if (isBlocked(ws.status)) cls += " blocked";
      else if (isCompleted(ws.status) || ws.status.toUpperCase() === "IDLE") cls += " " + completedClass(ws.age);

      html += '<div class="' + cls + '" data-id="' + esc(ws.id) + '" style="border-left-color:' + c + '">';
      html += '<div class="status-dot" style="background:' + c + '"></div>';
      html += '<span class="ws-name">' + esc(ws.id) + '</span>';
      if (ws.agent && !costLookup[ws.id]) {
        var agentLabel = ws.agent;
        var agentColor = "var(--text-muted)";
        if (agentLabel.indexOf("sonnet") >= 0) agentColor = "#a78bfa";
        else if (agentLabel.indexOf("opus") >= 0) agentColor = "#f59e0b";
        else if (agentLabel.indexOf("cursor") >= 0) agentColor = "#06b6d4";
        else if (agentLabel.indexOf("codex") >= 0) agentColor = "#10b981";
        html += '<span class="ws-model-badge" style="color:' + agentColor + ';border-color:' + agentColor + '44">' + esc(agentLabel) + '</span>';
      }
      html += '<span class="ws-step">' + esc(ws.task || "-") + '</span>';
      html += '<span class="shield" data-shield="' + esc(ws.id) + '"></span>';
      var taskCost = costLookup[ws.id];
      if (taskCost) {
        var mColor = MODEL_COLORS[taskCost.model] || "var(--text-muted)";
        html += '<span class="model-badge" style="color:' + mColor + ';background:' + mColor + '1f">' + esc(taskCost.model) + '</span>';
        var isEstimated = taskCost.source === "estimated" || taskCost.source === "unavailable";
        if (isEstimated) {
          html += '<span class="cost-badge" title="Estimated — Cursor/Codex tokens not tracked">~$' + taskCost.cost.toFixed(2) + ' <span class="est-tag">est</span></span>';
        } else {
          html += '<span class="cost-badge">$' + taskCost.cost.toFixed(2) + '</span>';
        }
      }
      html += '<span class="ws-badge" style="color:' + c + ';background:' + c + '22">' + esc(ws.status) + '</span>';
      if (ws.diff && ws.diff !== "-") html += '<span class="ws-agent">' + esc(ws.diff) + '</span>';
      html += '<span class="ws-agent">' + esc(ws.age) + '</span>';
      if (isBlocked(ws.status)) {
        html += '<button class="btn-accent" data-approve="' + esc(ws.id) + '" style="margin-left:auto">Approve</button>';
      }
      html += '</div>';
    });

    var list = document.getElementById("workspaceList");
    // Don't rebuild DOM if a workspace is expanded (would destroy the panel).
    if (document.querySelector(".workspace-expanded")) return;
    if (list._lastHTML !== html) {
      list._lastHTML = html;
      list.innerHTML = html;

      // Bind events
      document.querySelectorAll(".ws-row").forEach(function (el) {
        el.addEventListener("click", function (e) {
          if (e.target.closest("button, .shield")) return;
          var id = el.getAttribute("data-id");
          if (typeof window.expandWorkspace === "function") window.expandWorkspace(id);
        });
      });
      document.querySelectorAll("[data-approve]").forEach(function (btn) {
        btn.addEventListener("click", function (e) {
          e.stopPropagation();
          fetch("/api/workspaces/" + encodeURIComponent(btn.getAttribute("data-approve")) + "/approve", { method: "POST" });
        });
      });
    }
  }

  // --- Main render ---
  function render(data) {
    var json = JSON.stringify(data);
    // Always refresh counters and safety (approval counts change independently).
    renderCounters(data);
    (data || []).forEach(function (ws) { fetchSafety(ws.id); });
    if (json !== lastJSON) {
      lastJSON = json;
      renderWorkspaces(data);
    }
  }

  // --- Export ---
  document.getElementById("exportAudit").addEventListener("click", function () {
    window.location.href = "/api/audit/export?format=csv&since=168h";
  });

  // --- Poll ---
  function poll() {
    fetch("/api/workspaces").then(function (r) { return r.json(); }).then(render).catch(function () {});
    fetch("/api/events").then(function (r) { return r.json(); }).then(function (events) {
      if (typeof renderActivity === "function") renderActivity(events);
    }).catch(function () {});
    fetch("/api/cost").then(function (r) { return r.json(); }).then(function (data) {
      var oldTasks = JSON.stringify(costCache.tasks);
      costCache = data;
      renderCostPanel(data);
      // Re-render workspace list when cost data changes (new badges).
      if (JSON.stringify(data.tasks) !== oldTasks) {
        lastJSON = "";
      }
    }).catch(function () {});
    setTimeout(poll, 4000);
  }
  poll();
})();
