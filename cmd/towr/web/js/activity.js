/* Activity feed — clean, grouped audit trail */
(function() {
  "use strict";

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  function relativeTime(ts) {
    var diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
    if (diff < 10) return "just now";
    if (diff < 60) return diff + "s ago";
    if (diff < 3600) return Math.floor(diff / 60) + "m ago";
    if (diff < 86400) return Math.floor(diff / 3600) + "h ago";
    return Math.floor(diff / 86400) + "d ago";
  }

  function truncate(s, n) {
    if (!s) return "";
    return s.length > n ? s.substring(0, n) + "..." : s;
  }

  function isBypass(kind) {
    return kind && (kind.indexOf("forced") !== -1 || kind.indexOf("hooks_skipped") !== -1);
  }

  // Collapse consecutive approval events into a single line
  function groupEvents(events) {
    var result = [];
    var approvalRun = null;

    for (var i = 0; i < events.length; i++) {
      var ev = events[i];
      if (ev.kind === "task.approved") {
        if (approvalRun && approvalRun.ws === ev.workspace_id) {
          approvalRun.count++;
          approvalRun.last = ev;
        } else {
          if (approvalRun) result.push(approvalRun);
          approvalRun = { type: "approval_group", ws: ev.workspace_id, count: 1, first: ev, last: ev };
        }
      } else {
        if (approvalRun) { result.push(approvalRun); approvalRun = null; }
        result.push({ type: "event", ev: ev });
      }
    }
    if (approvalRun) result.push(approvalRun);
    return result;
  }

  function describeEvent(ev) {
    var d = ev.data || {};
    var summary = d.summary || d.message || "";
    // Clean up raw terminal output from summaries
    if (summary.indexOf("\u23FA") >= 0 || summary.indexOf("\u2502") >= 0 || summary.indexOf("─") >= 0) {
      // Raw terminal garbage — extract first meaningful sentence
      var clean = summary.replace(/[\u23FA\u2502─┌┐└┘│▶▷▸▹►▻⏺•●○◌◍◎●]+/g, " ").replace(/\s+/g, " ").trim();
      summary = truncate(clean, 120);
    }
    switch (ev.kind) {
      case "task.dispatched": return "Dispatched";
      case "task.started": return "Working";
      case "task.completed": return truncate(summary, 100) || "Completed";
      case "task.failed": return "Failed: " + truncate(summary, 80);
      case "task.blocked": return "Blocked" + (d.dialog ? ": " + truncate(d.dialog, 60) : "");
      case "workspace.created": return "Created";
      case "workspace.landed": return "Landed";
      default: return ev.kind;
    }
  }

  // Toggle
  document.getElementById("actToggle").addEventListener("click", function() {
    this.classList.toggle("open");
    document.getElementById("actFeed").classList.toggle("open");
  });

  var lastCount = 0;
  window.renderActivity = function(events) {
    var countEl = document.getElementById("actCount");
    var feed = document.getElementById("actFeed");
    if (!events || events.length === lastCount) return;
    lastCount = events.length;
    countEl.textContent = "(" + events.length + ")";

    var grouped = groupEvents(events);
    var html = "";

    for (var i = 0; i < grouped.length; i++) {
      var item = grouped[i];

      if (item.type === "approval_group") {
        html += '<div class="evt-row evt-approval-row">';
        html += '<span class="evt-ts">' + esc(relativeTime(item.first.ts)) + '</span>';
        html += '<span class="evt-icon" style="color:var(--accent-green)">✓</span>';
        html += '<span class="evt-ws">' + esc(item.ws) + '</span>';
        html += '<span class="evt-desc">' + item.count + ' auto-approval' + (item.count > 1 ? 's' : '') + '</span>';
        html += '</div>';
        continue;
      }

      var ev = item.ev;
      var kind = ev.kind || "";
      var bypass = isBypass(kind);
      var cls = "evt-row";
      if (bypass) cls += " evt-bypass";

      var icon, iconColor;
      switch (kind) {
        case "task.completed":
        case "workspace.landed":
          icon = "●"; iconColor = "var(--accent-green)"; break;
        case "task.failed":
          icon = "●"; iconColor = "var(--accent-red)"; break;
        case "task.dispatched":
        case "task.started":
          icon = "●"; iconColor = "var(--accent-blue)"; break;
        case "task.blocked":
          icon = "⚠"; iconColor = "var(--accent-yellow)"; break;
        default:
          icon = "●"; iconColor = "var(--text-muted)"; break;
      }
      if (bypass) { icon = "⚠"; iconColor = "var(--accent-red)"; }

      html += '<div class="' + cls + '">';
      html += '<span class="evt-ts">' + esc(relativeTime(ev.ts)) + '</span>';
      html += '<span class="evt-icon" style="color:' + iconColor + '">' + icon + '</span>';
      html += '<span class="evt-ws">' + esc(ev.workspace_id || "-") + '</span>';
      html += '<span class="evt-desc">' + esc(describeEvent(ev));
      if (bypass) html += ' <span class="evt-bypass-tag">[BYPASS]</span>';
      html += '</span>';
      html += '</div>';
    }

    feed.innerHTML = html;
  };
})();
