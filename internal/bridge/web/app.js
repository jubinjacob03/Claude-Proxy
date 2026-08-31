const $ = (sel) => document.querySelector(sel);
const form = $("#settingsForm");

function toast(msg, kind) {
  const t = $("#toast");
  t.textContent = msg;
  t.className = "toast " + (kind || "");
  t.hidden = false;
  clearTimeout(t._timer);
  t._timer = setTimeout(() => {
    t.hidden = true;
  }, 2600);
}

function testKeyHeaders() {
  const key = $("#testKey").value.trim();
  const h = { "Content-Type": "application/json" };
  if (key) h["x-api-key"] = key;
  return h;
}

async function loadHealth() {
  try {
    const r = await fetch("/health");
    const h = await r.json();
    $("#statusDot").className = "dot ok";
    const up = h.config ? h.config.upstream_base_url : "";
    const fmt = h.config ? h.config.upstream_format : "";
    $("#statusText").textContent = `v${h.version} · ${fmt} · ${up}`;
    if (h.validation && !h.validation.ok) {
      $("#statusDot").className = "dot bad";
      $("#statusText").textContent = h.validation.problems.join("; ");
    }
  } catch (e) {
    $("#statusDot").className = "dot bad";
    $("#statusText").textContent = "proxy unreachable";
  }
}

async function loadConfig() {
  const r = await fetch("/config");
  const c = await r.json();
  form.upstream_base_url.value = c.upstream_base_url || "";
  form.upstream_format.value = c.upstream_format || "anthropic";
  form.auth_token.value = "";
  form.upstream_api_key.value = "";
  form.default_model.value = c.default_model || "";
  form.default_max_tokens.value = c.default_max_tokens ?? 4096;
  form.model_map.value = c.model_map || "";
  form.stream_idle_ping_seconds.value = c.stream_idle_ping_seconds ?? 15;
  form.request_timeout_seconds.value = c.request_timeout_seconds ?? 0;
  form.log_level.value = c.log_level || "info";
  form.log_format.value = c.log_format || "text";
  form.log_bodies.checked = !!c.log_bodies;
  $("#keyState").textContent = c.upstream_api_key_set
    ? `current: ${c.upstream_api_key_masked}`
    : "not set";
  $("#envPath").textContent = c.env_path ? `.env: ${c.env_path}` : "";
  $("#testModel").value = c.default_model || "";
  renderSnippets(c.default_model || "claude-opus-4-8");
}

async function saveConfig(ev) {
  ev.preventDefault();
  const payload = {
    upstream_base_url: form.upstream_base_url.value.trim(),
    upstream_format: form.upstream_format.value,
    auth_token: form.auth_token.value,
    default_model: form.default_model.value.trim(),
    model_map: form.model_map.value.trim(),
    default_max_tokens: Number(form.default_max_tokens.value) || 4096,
    stream_idle_ping_seconds: Number(form.stream_idle_ping_seconds.value) || 0,
    request_timeout_seconds: Number(form.request_timeout_seconds.value) || 0,
    log_level: form.log_level.value,
    log_format: form.log_format.value,
    log_bodies: form.log_bodies.checked,
    persist: form.persist.checked,
  };
  const key = form.upstream_api_key.value.trim();
  if (key) payload.upstream_api_key = key;

  try {
    const r = await fetch("/config", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const res = await r.json();
    if (!r.ok) throw new Error(res.error ? res.error.message : "save failed");
    toast(
      res.persisted ? "Saved and written to .env" : "Applied (not persisted)",
      "ok",
    );
    await loadConfig();
    await loadHealth();
  } catch (e) {
    toast(e.message, "bad");
  }
}

async function loadModels() {
  const box = $("#models");
  box.innerHTML = "<div class='muted'>loading…</div>";
  try {
    const r = await fetch("/v1/models", { headers: testKeyHeaders() });
    if (!r.ok) throw new Error("HTTP " + r.status);
    const data = await r.json();
    const list = (data.data || []).map((m) => m.id).filter(Boolean);
    if (!list.length) {
      box.innerHTML = "<div class='muted'>no models returned</div>";
      return;
    }
    box.innerHTML = "";
    for (const id of list) {
      const el = document.createElement("div");
      el.className = "model";
      el.innerHTML = `<div class="id">${id}</div><div class="tag">click to use in test</div>`;
      el.onclick = () => {
        $("#testModel").value = id;
        toast("test model set to " + id, "ok");
      };
      box.appendChild(el);
    }
  } catch (e) {
    box.innerHTML = `<div class='muted'>failed: ${e.message} (set the proxy key if one is configured)</div>`;
  }
}

async function runTest() {
  const out = $("#testOut");
  const meta = $("#testMeta");
  const model = $("#testModel").value.trim() || "claude-opus-4-8";
  const prompt = $("#testPrompt").value;
  meta.textContent = "sending…";
  out.hidden = true;
  const started = performance.now();
  try {
    const r = await fetch("/v1/messages", {
      method: "POST",
      headers: testKeyHeaders(),
      body: JSON.stringify({
        model,
        max_tokens: 1024,
        messages: [{ role: "user", content: prompt }],
      }),
    });
    const ms = Math.round(performance.now() - started);
    const body = await r.json();
    if (!r.ok)
      throw new Error(body.error ? body.error.message : "HTTP " + r.status);
    const text = (body.content || [])
      .filter((b) => b.type === "text")
      .map((b) => b.text)
      .join("");
    const u = body.usage || {};
    meta.textContent = `${ms} ms · in ${u.input_tokens ?? "?"} / out ${u.output_tokens ?? "?"} · ${body.model || model}`;
    out.textContent = text || JSON.stringify(body, null, 2);
    out.hidden = false;
  } catch (e) {
    meta.textContent = "error";
    out.textContent = e.message;
    out.hidden = false;
  }
}

function renderSnippets(model) {
  const origin = window.location.origin;
  const items = [
    {
      title: "Trae — Anthropic-compatible (recommended)",
      body: `Provider: Anthropic (compatible)\nBase URL: ${origin}\nAPI Key:  any value (or your auth token)\nModel:    ${model}`,
    },
    {
      title: "Trae — OpenAI-compatible",
      body: `Provider: OpenAI (compatible)\nBase URL: ${origin}/v1\nAPI Key:  any value (or your auth token)\nModel:    ${model}`,
    },
    {
      title: "Claude Code (PowerShell)",
      body: `$env:ANTHROPIC_BASE_URL   = "${origin}"\n$env:ANTHROPIC_AUTH_TOKEN = "any-or-auth-token"\n$env:ANTHROPIC_MODEL      = "${model}"\nclaude`,
    },
  ];
  const box = $("#snippets");
  box.innerHTML = "";
  for (const it of items) {
    const wrap = document.createElement("div");
    wrap.className = "snippet";
    wrap.innerHTML = `<h3>${it.title}</h3><pre><button class="copy">copy</button>${escapeHtml(it.body)}</pre>`;
    wrap.querySelector(".copy").onclick = () => {
      navigator.clipboard.writeText(it.body).then(() => toast("copied", "ok"));
    };
    box.appendChild(wrap);
  }
}

function escapeHtml(s) {
  return s.replace(
    /[&<>]/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" })[c],
  );
}

form.addEventListener("submit", saveConfig);
$("#reloadBtn").onclick = () => {
  loadConfig();
  loadHealth();
  toast("reloaded", "ok");
};
$("#loadModels").onclick = loadModels;
$("#sendTest").onclick = runTest;

loadConfig();
loadHealth();
setInterval(loadHealth, 10000);
