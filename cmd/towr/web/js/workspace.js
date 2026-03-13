(function() {
  "use strict";

  var activeId = null;
  var evtSource = null;
  var rawMode = false;

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  // Exposed globally — app.js calls this on row click.
  window.expandWorkspace = function(id) {
    // Toggle off if same row clicked.
    if (activeId === id) {
      collapseWorkspace();
      return;
    }
    collapseWorkspace();

    activeId = id;
    rawMode = false;

    // Find the clicked row and insert expanded view after it.
    var row = document.querySelector('.ws-row[data-id="' + id + '"]');
    if (!row) return;
    row.classList.add("expanded");

    var panel = document.createElement("div");
    panel.className = "workspace-expanded";
    panel.innerHTML =
      '<div class="expanded-header">' +
        '<span class="expanded-title">' + esc(id) + '</span>' +
        '<button class="btn-ghost expanded-close">✕</button>' +
      '</div>' +
      '<div class="expanded-body"><span style="color:var(--text-muted)">Connecting...</span></div>' +
      '<div class="action-bar">' +
        '<input class="action-input" placeholder="Send a message...">' +
        '<button class="btn-accent action-send">Send</button>' +
        '<button class="btn-ghost action-raw">Raw terminal</button>' +
      '</div>';
    row.after(panel);

    // Bind events
    panel.querySelector(".expanded-close").addEventListener("click", collapseWorkspace);
    panel.querySelector(".action-send").addEventListener("click", function() {
      var input = panel.querySelector(".action-input");
      if (!input.value || !activeId) return;
      fetch("/api/workspaces/" + encodeURIComponent(activeId) + "/send", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: input.value })
      });
      input.value = "";
    });
    panel.querySelector(".action-input").addEventListener("keydown", function(e) {
      if (e.key === "Enter") panel.querySelector(".action-send").click();
    });
    panel.querySelector(".action-raw").addEventListener("click", function() {
      rawMode = !rawMode;
      this.classList.toggle("active", rawMode);
    });

    // Connect SSE
    connectSSE(id, panel.querySelector(".expanded-body"));
  };

  function collapseWorkspace() {
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    var panel = document.querySelector(".workspace-expanded");
    if (panel) panel.remove();
    document.querySelectorAll(".ws-row.expanded").forEach(function(el) {
      el.classList.remove("expanded");
    });
  }
  window.collapseWorkspace = collapseWorkspace;

  function connectSSE(id, body) {
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    evtSource.onmessage = function(e) {
      var text = e.data;
      // Remember if user scrolled up (don't auto-scroll if they're reading)
      var isAtBottom = body.scrollHeight - body.scrollTop - body.clientHeight < 50;

      if (rawMode) {
        // Raw mode: append new content, keep existing
        var pre = body.querySelector(".raw-output");
        if (!pre) {
          pre = document.createElement("pre");
          pre.className = "raw-output";
          body.innerHTML = "";
          body.appendChild(pre);
        }
        pre.textContent = text;
      } else {
        var steps = parseSteps(text);
        if (steps.length > 0) {
          body.innerHTML = renderSteps(steps);
        } else {
          // No structured steps — show as formatted raw
          var pre = body.querySelector(".raw-output");
          if (!pre) {
            pre = document.createElement("pre");
            pre.className = "raw-output";
            body.innerHTML = "";
            body.appendChild(pre);
          }
          pre.textContent = text;
        }
      }
      // Only auto-scroll if user was at the bottom
      if (isAtBottom) body.scrollTop = body.scrollHeight;
    };
    evtSource.onerror = function() {
      if (evtSource && evtSource.readyState === EventSource.CLOSED) {
        body.innerHTML += '<div style="color:var(--text-muted);padding:8px 0">Stream ended</div>';
      }
    };
  }

  // Step parser
  function parseSteps(text) {
    var lines = text.split("\n");
    var steps = [];
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      // Claude: ⏺ prefix, Codex: • prefix
      if (line.match(/^[\u23FA\u2B58\u25CF\u2022\u2B24]/) || line.match(/^\u2022/) || line.indexOf("\u23FA") === 0) {
        var label = line.replace(/^[\u23FA\u2B58\u25CF\u2022\u2B24\s]+/, "").trim();
        if (label) {
          var kind = classifyStep(label);
          var done = line.indexOf("\u2713") >= 0 || line.indexOf("✓") >= 0;
          steps.push({ label: label, kind: kind, done: done });
        }
      }
    }
    return steps;
  }

  function classifyStep(text) {
    if (/\b(Read|Explored|Searched|Globbed)\b/.test(text)) return "read";
    if (/\b(Write|Edit|Added|Created|Update)\b/.test(text)) return "write";
    if (/\b(Bash\(go test|test|Test)\b/.test(text)) return "test";
    if (/\b(Bash\(git|commit|Commit)\b/.test(text)) return "commit";
    if (/\b(Bash\(gh|pr|PR)\b/.test(text)) return "pr";
    return "action";
  }

  function renderSteps(steps) {
    var html = '<div class="step-list">';
    for (var i = 0; i < steps.length; i++) {
      var s = steps[i];
      var icon = s.done ? '<span style="color:var(--accent-green)">✓</span>' :
        (i === steps.length - 1 ? '<span style="color:var(--accent-blue)">▶</span>' :
        '<span style="color:var(--accent-green)">✓</span>');
      html += '<div class="step-item">';
      html += '<span class="step-icon">' + icon + '</span>';
      html += '<span class="step-label">' + esc(s.label) + '</span>';
      html += '</div>';
    }
    html += '</div>';
    return html;
  }

  // Close on Escape
  document.addEventListener("keydown", function(e) {
    if (e.key === "Escape" && activeId) collapseWorkspace();
  });
})();
