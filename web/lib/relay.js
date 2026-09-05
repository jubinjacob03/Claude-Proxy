// Server-only. This module must never be imported from a Client Component:
// it holds the admin token that talks to the relay's /admin/* API, and that
// token must never reach the browser.
import "server-only";

function relayBaseURL() {
  const url = process.env.RELAY_BASE_URL;
  if (!url) throw new Error("RELAY_BASE_URL is not configured");
  return url.replace(/\/$/, "");
}

function adminToken() {
  const token = process.env.RELAY_ADMIN_TOKEN;
  if (!token) throw new Error("RELAY_ADMIN_TOKEN is not configured");
  return token;
}

export class RelayError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

async function call(path, init) {
  const res = await fetch(relayBaseURL() + path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      "X-Admin-Token": adminToken(),
      ...(init?.headers || {}),
    },
    cache: "no-store",
  });
  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new RelayError(
      res.status,
      body.message || `relay returned ${res.status}`,
    );
  }
  return body;
}

export const relay = {
  listLicenses: () => call("/admin/licenses"),
  createLicenses: (quotaCents, note, count = 1) =>
    call("/admin/licenses", {
      method: "POST",
      body: JSON.stringify({ quota_cents: quotaCents, note, count }),
    }),
  getLicense: (id) => call(`/admin/licenses/${id}`),
  pauseLicense: (id) => call(`/admin/licenses/${id}/pause`, { method: "POST" }),
  resumeLicense: (id) =>
    call(`/admin/licenses/${id}/resume`, { method: "POST" }),
  resetHwid: (id) =>
    call(`/admin/licenses/${id}/reset-hwid`, { method: "POST" }),
  setQuota: (id, quotaCents) =>
    call(`/admin/licenses/${id}/quota`, {
      method: "POST",
      body: JSON.stringify({ quota_cents: quotaCents }),
    }),

  listPool: (query = {}) => {
    const q = new URLSearchParams();
    if (query.poolGroup) q.set("pool_group", query.poolGroup);
    const suffix = q.size ? `?${q.toString()}` : "";
    return call(`/admin/pool${suffix}`);
  },
  addPoolKey: (label, secret, provider, poolGroup, balanceCents) =>
    call("/admin/pool", {
      method: "POST",
      body: JSON.stringify({
        label,
        secret,
        provider,
        pool_group: poolGroup,
        balance_cents: balanceCents,
      }),
    }),
  getPoolKey: (id) => call(`/admin/pool/${id}`),
  topUpPoolKey: (id, balanceCents) =>
    call(`/admin/pool/${id}/topup`, {
      method: "POST",
      body: JSON.stringify({ balance_cents: balanceCents }),
    }),
  rotatePoolKey: (id, label, secret, balanceCents) =>
    call(`/admin/pool/${id}/rotate`, {
      method: "POST",
      body: JSON.stringify({ label, secret, balance_cents: balanceCents }),
    }),
  enablePoolKey: (id) => call(`/admin/pool/${id}/enable`, { method: "POST" }),
  disablePoolKey: (id) => call(`/admin/pool/${id}/disable`, { method: "POST" }),
  deletePoolKey: (id) => call(`/admin/pool/${id}/delete`, { method: "POST" }),
  getUpstreamUsage: (id) => call(`/admin/pool/${id}/upstream-usage`),

  listEndpointProfiles: () => call("/admin/endpoints"),
  saveEndpointProfile: (name, claudeBaseURL, poolGroup, active = false, billingMode, perRequestCostCents, inputCostPerM, outputCostPerM) =>
    call("/admin/endpoints", {
      method: "POST",
      body: JSON.stringify({
        name,
        claude_base_url: claudeBaseURL,
        pool_group: poolGroup,
        active,
        billing_mode: billingMode,
        per_request_cost_cents: perRequestCostCents,
        input_cost_per_m: inputCostPerM,
        output_cost_per_m: outputCostPerM,
      }),
    }),
  getEndpointProfile: (name) =>
    call(`/admin/endpoints/${encodeURIComponent(name)}`),
  activateEndpointProfile: (name) =>
    call(`/admin/endpoints/${encodeURIComponent(name)}/activate`, {
      method: "POST",
    }),
  deleteEndpointProfile: (name) =>
    call(`/admin/endpoints/${encodeURIComponent(name)}/delete`, {
      method: "POST",
    }),

  usage: (filters = {}) => {
    const q = new URLSearchParams();
    if (filters.license_id) q.set("license_id", filters.license_id);
    if (filters.q) q.set("q", filters.q);
    if (filters.status) q.set("status", filters.status);
    const suffix = q.size ? `?${q.toString()}` : "";
    return call(`/admin/usage${suffix}`);
  },
};
