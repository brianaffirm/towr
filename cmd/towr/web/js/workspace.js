(function() {
  "use strict";

  var activeId = null;
  var evtSource = null;
  var updateCount = 0;
  var rawMode = false;

  // Step parser: classify lines by marker and content.
  // ⏺ = active step, • = completed step
  var STEP_RE = /^[\s]*([\u2B58\u25CF\u2022])[\s]+(.+)$/;
  var KIND_PATTERNS = [
    { re: /\b(read|reading|Read)\b/, kind: "read" },
    { re: /\b(writ|edit|creat|Write|Edit)\b/, kind: "write" },
    { re: /\b(test|Test|assert|spec)\b/, kind: "test" },
    { re: /\b(commit|push|merge|Commit)\b/, kind: "commit" }
  ];

  function classifyStep(text) {
    for (var i = 0; i < KIND_PATTERNS.length; i++) {
      if (KIND_PATTERNS[i].re.test(text)) return KIND_PATTERNS[i].kind;
    }
    return "read";
  }

  function parseSteps(raw) {
    var lines = raw.split("\n");
    var steps = [];
    for (var i = 0; i < lines.length; i++) {
      var m = lines[i].match(STEP_RE);
      if (m) {
        var marker = m[1];
        var text = m[2].trim();
        var active = marker === "\u2B58" || marker === "\u25CF";
        steps.push({ text: text, active: active, kind: classifyStep(text) });
      }
    }
    return steps;
  }

  function esc(s) {
    var d = document.createElement("span");
    d.textContent = s;
    return d.innerHTML;
  }

  function renderSteps(steps) {
    var html = '<ul class="ws-steps">';
    for (var i = 0; i < steps.length; i++) {
      var s = steps[i];
      var cls = s.active ? "ws-step active" : "ws-step";
      var icon = s.active ? "\u25B6" : "\u2713";
      html += '<li class="' + cls + '">';
      html += '<span class="ws-step-icon ' + s.kind + '">' + icon + '</span>';
      html += '<span>' + esc(s.text) + '</span>';
      html += '</li>';
    }
    html += '</ul>';
    return html;
  }

  function renderRaw(text) {
    return '<pre style="margin:0;white-space:pre-wrap;font-size:0.75rem;color:#8b949e">' + esc(text) + '</pre>';
  }

  // Exposed globally so app.js card clicks can trigger it.
  window.expandWorkspace = function(id) {
    var panel = document.getElementById("termPanel");
    var body = document.getElementById("termBody");
    var title = document.getElementById("termTitle");

    // Toggle off if same id clicked again.
    if (activeId === id && panel.classList.contains("open")) {
      closePanel();
      return;
    }

    activeId = id;
    updateCount = 0;
    rawMode = false;
    panel.classList.add("open");
    title.textContent = id;
    body.innerHTML = '<span style="color:#484f58">connecting\u2026</span>';

    // Render action bar
    renderActionBar();

    // Highlight active card
    document.querySelectorAll(".card").forEach(function(el) {
      el.classList.toggle("active", el.getAttribute("data-id") === id);
    });

    // Connect SSE
    if (evtSource) { evtSource.close(); evtSource = null; }
    evtSource = new EventSource("/stream/" + encodeURIComponent(id));
    evtSource.onmessage = function(e) {
      updateCount++;
      var text = e.data.replace(/\n$/, "");
      var steps = parseSteps(text);

      // Structured view if we have steps and haven't exceeded threshold, or not in raw mode.
      if (!rawMode && steps.length > 0 && updateCount <= 3) {
        body.innerHTML = renderSteps(steps);
      } else {
        // After 3 updates or raw mode, show raw terminal output.
        if (!rawMode && updateCount > 3 && steps.length > 0) {
          rawMode = false; // keep structured if steps exist, unless user toggled
          body.innerHTML = renderSteps(steps);
        } else {
          body.innerHTML = renderRaw(text);
        }
      }
      body.scrollTop = body.scrollHeight;
    };
    evtSource.onerror = function() {
      body.innerHTML += '\n<span style="color:#f85149">[stream disconnected]</span>';
    };
  };

  function renderActionBar() {
    var panel = document.getElementById("termPanel");
    var existing = panel.querySelector(".ws-action-bar");
    if (existing) existing.remove();

    var bar = document.createElement("div");
    bar.className = "ws-action-bar";
    bar.innerHTML =
      '<input id="wsSendInput" placeholder="message\u2026">' +
      '<button id="wsSendBtn">send</button>' +
      '<button id="wsRawToggle"' + (rawMode ? ' class="active"' : '') + '>raw</button>';
    panel.appendChild(bar);

    document.getElementById("wsSendBtn").addEventListener("click", function() {
      var input = document.getElementById("wsSendInput");
      if (!input.value || !activeId) return;
      fetch("/api/workspaces/" + encodeURIComponent(activeId) + "/send", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: input.value })
      });
      input.value = "";
    });

    document.getElementById("wsSendInput").addEventListener("keydown", function(e) {
      if (e.key === "Enter") document.getElementById("wsSendBtn").click();
    });

    document.getElementById("wsRawToggle").addEventListener("click", function() {
      rawMode = !rawMode;
      this.classList.toggle("active", rawMode);
    });
  }

  function closePanel() {
    document.getElementById("termPanel").classList.remove("open");
    if (evtSource) { evtSource.close(); evtSource = null; }
    activeId = null;
    updateCount = 0;
    var bar = document.querySelector(".ws-action-bar");
    if (bar) bar.remove();
    document.querySelectorAll(".card.active").forEach(function(el) {
      el.classList.remove("active");
    });
  }

  var termClose = document.getElementById("termClose");
  if (termClose) termClose.addEventListener("click", closePanel);
})();
