/* Activity feed — reverse-chronological audit trail for towr dashboard */
(function() {
  "use strict";

  var DOT_CLASSES = {
    "task.dispatched": "evt-dot-blue", "task.started": "evt-dot-blue",
    "task.completed": "evt-dot-green", "workspace.landed": "evt-dot-green",
    "task.approved": "evt-dot-green",
    "task.failed": "evt-dot-red",
    "task.blocked": "evt-dot-yellow"
  };
  var DEFAULT_DOT_CLASS = "evt-dot-muted";

  var prevIds = {};

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

  function describeEvent(ev) {
    var d = ev.data || {};
    switch (ev.kind) {
      case "task.dispatched":  return "Dispatched to " + (ev.workspace_id || "?");
      case "task.started":     return "Started working";
      case "task.completed":   return "Completed: " + (d.summary || d.message || "-");
      case "task.failed":      return "Failed: " + (d.summary || d.message || "-");
      case "task.blocked":     return "Blocked: " + (d.dialog || d.message || "-");
      case "task.approved":    return "Approved: " + (d.summary || d.message || "-");
      case "workspace.created": return "Workspace created";
      case "workspace.landed": return "Landed (merged)";
      default:                 return ev.kind;
    }
  }

  function eventKey(ev) {
    return (ev.id || "") + ":" + ev.kind + ":" + ev.ts;
  }

  function isBypassEvent(kind) {
    return kind.indexOf("forced") !== -1 || kind.indexOf("hooks_skipped") !== -1;
  }

  // Toggle drawer
  document.getElementById("actToggle").addEventListener("click", function() {
    this.classList.toggle("open");
    document.getElementById("actFeed").classList.toggle("open");
  });

  window.renderActivity = function(events) {
    var feed = document.getElementById("actFeed");
    var countEl = document.getElementById("actCount");
    var list = events || [];

    countEl.textContent = "(" + list.length + ")";

    var newIds = {};
    var html = "";

    list.forEach(function(ev) {
      var key = eventKey(ev);
      newIds[key] = true;
      var isNew = !prevIds[key];
      var kind = ev.kind || "";
      var bypass = isBypassEvent(kind);
      var isApproval = kind === "task.approved" || kind === "queue.resolved";
      var isBlock = kind.indexOf("blocked") !== -1;
      var dotClass = DOT_CLASSES[kind] || DEFAULT_DOT_CLASS;
      var ws = ev.workspace_id || "-";
      var rowClass = "evt-row" + (isNew ? " evt-new" : "") + (bypass ? " evt-bypass" : "");

      if (bypass) dotClass = "evt-dot-red";

      html += '<div class="' + rowClass + '">';
      html += '<span class="evt-ts">' + esc(relativeTime(ev.ts)) + '</span>';
      if (isApproval) {
        html += '<span class="evt-dot evt-approval">\u2713</span>';
      } else if (isBlock) {
        html += '<span class="evt-dot evt-block">\u2717</span>';
      } else {
        html += '<span class="evt-dot ' + dotClass + '">\u25CF</span>';
      }
      html += '<span class="evt-ws">' + esc(ws) + '</span>';
      var desc = describeEvent(ev);
      if (isApproval && ev.data && ev.data.command) desc = "Approved: " + ev.data.command;
      if (isBlock && ev.data && ev.data.command) desc = "Blocked: " + ev.data.command;
      html += '<span class="evt-desc">' + esc(desc);
      if (bypass) html += ' <span class="evt-bypass-tag">[BYPASS]</span>';
      html += '</span>';
      html += '</div>';
    });

    feed.innerHTML = html;
    prevIds = newIds;
  };
})();
