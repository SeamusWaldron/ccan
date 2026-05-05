(function () {
  "use strict";

  const data = window.__CUA_DATA__;
  if (!data) return;

  const daily = data.daily || [];
  const limitEvents = data.limit_events || [];

  // helpers
  function labels(rows) { return rows.map(r => r.date || r.Date); }

  // Chart: Usage Over Time
  (function () {
    const ctx = document.getElementById("chartUsage");
    if (!ctx || !daily.length) return;
    new Chart(ctx, {
      type: "line",
      data: {
        labels: daily.map(r => r.date),
        datasets: [
          {
            label: "User messages",
            data: daily.map(r => r.user_message_count),
            borderColor: "#3b82f6",
            backgroundColor: "rgba(59,130,246,0.08)",
            tension: 0.3,
            fill: true,
          },
          {
            label: "Assistant messages",
            data: daily.map(r => r.assistant_message_count),
            borderColor: "#10b981",
            backgroundColor: "rgba(16,185,129,0.08)",
            tension: 0.3,
            fill: true,
          },
          {
            label: "Tool calls",
            data: daily.map(r => r.tool_call_count),
            borderColor: "#f59e0b",
            backgroundColor: "rgba(245,158,11,0.08)",
            tension: 0.3,
            fill: true,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: "index", intersect: false },
        plugins: { legend: { position: "top" } },
        scales: { y: { beginAtZero: true } },
      },
    });
  })();

  // Chart: Limit Events Over Time
  (function () {
    const ctx = document.getElementById("chartLimits");
    if (!ctx || !daily.length) return;
    new Chart(ctx, {
      type: "bar",
      data: {
        labels: daily.map(r => r.date),
        datasets: [
          {
            label: "Limit events",
            data: daily.map(r => r.limit_event_count),
            backgroundColor: "rgba(220,38,38,0.7)",
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { position: "top" } },
        scales: { y: { beginAtZero: true, ticks: { precision: 0 } } },
      },
    });
  })();

  // Chart: Sessions Per Day
  (function () {
    const ctx = document.getElementById("chartSessions");
    if (!ctx || !daily.length) return;
    new Chart(ctx, {
      type: "bar",
      data: {
        labels: daily.map(r => r.date),
        datasets: [
          {
            label: "Sessions",
            data: daily.map(r => r.session_count),
            backgroundColor: "rgba(99,102,241,0.7)",
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { position: "top" } },
        scales: { y: { beginAtZero: true, ticks: { precision: 0 } } },
      },
    });
  })();

  // Populate limit events table
  (function () {
    const tbody = document.getElementById("limitEventsBody");
    if (!tbody) return;
    if (!limitEvents.length) {
      tbody.innerHTML = '<tr><td colspan="5" style="color:#aaa">No limit events detected.</td></tr>';
      return;
    }
    tbody.innerHTML = limitEvents.map(e => `
      <tr>
        <td>${e.timestamp || "—"}</td>
        <td><span class="tag ${e.classification}">${e.classification}</span></td>
        <td>${e.matched_pattern}</td>
        <td>${(e.confidence * 100).toFixed(0)}%</td>
        <td style="max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
            title="${esc(e.redacted_excerpt)}">${esc(e.redacted_excerpt)}</td>
      </tr>`).join("");
  })();

  // Populate sessions table
  (function () {
    const tbody = document.getElementById("sessionsBody");
    if (!tbody) return;
    const sessions = data.sessions || [];
    if (!sessions.length) {
      tbody.innerHTML = '<tr><td colspan="7" style="color:#aaa">No sessions found.</td></tr>';
      return;
    }
    tbody.innerHTML = sessions.map(s => `
      <tr>
        <td>${s.started_at || "—"}</td>
        <td>${fmtDur(s.duration_seconds)}</td>
        <td>${s.user_message_count}</td>
        <td>${s.assistant_message_count}</td>
        <td>${s.tool_call_count}</td>
        <td>${fmtTokens(s)}</td>
        <td>${s.limit_event_count > 0 ? `<span class="tag hard_limit">${s.limit_event_count}</span>` : "—"}</td>
      </tr>`).join("");
  })();

  function esc(s) {
    return (s || "").replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
  }
  function fmtDur(sec) {
    if (!sec) return "—";
    const m = Math.floor(sec / 60);
    const s = sec % 60;
    return m > 0 ? `${m}m ${s}s` : `${s}s`;
  }
  function fmtTokens(s) {
    if (s.known_total_tokens > 0) return s.known_total_tokens.toLocaleString();
    if (s.estimated_total_tokens > 0) return `<span class="est">~${s.estimated_total_tokens.toLocaleString()}</span>`;
    return "—";
  }
})();
