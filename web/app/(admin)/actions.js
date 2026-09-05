"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { relay } from "@/lib/relay";
import { clearSession } from "@/lib/auth";
import { centsFromDollarsInput } from "@/lib/format";

export async function logoutAction() {
  await clearSession();
  redirect("/login");
}

export async function mintLicenseAction(_prevState, formData) {
  const dollars = String(formData.get("quota_dollars") || "");
  const note = String(formData.get("note") || "").trim();
  const count = Math.max(1, Number.parseInt(formData.get("count"), 10) || 1);

  const cents = centsFromDollarsInput(dollars);
  if (!cents)
    return { error: "Enter a quota greater than zero.", created: null };

  const { created } = await relay.createLicenses(cents, note, count);
  revalidatePath("/licenses");
  return { error: null, created };
}

export async function pauseLicenseAction(id) {
  await relay.pauseLicense(id);
  revalidatePath("/licenses");
}

export async function resumeLicenseAction(id) {
  await relay.resumeLicense(id);
  revalidatePath("/licenses");
}

export async function resetHwidAction(id) {
  await relay.resetHwid(id);
  revalidatePath("/licenses");
}

export async function setQuotaAction(id, formData) {
  const cents = centsFromDollarsInput(
    String(formData.get("quota_dollars") || ""),
  );
  if (!cents) return;
  await relay.setQuota(id, cents);
  revalidatePath("/licenses");
}

export async function addPoolKeyAction(formData) {
  const label = String(formData.get("label") || "").trim();
  const secret = String(formData.get("secret") || "").trim();
  const provider = String(formData.get("provider") || "claude");
  const poolGroup = String(formData.get("pool_group") || "").trim();
  const cents = centsFromDollarsInput(
    String(formData.get("balance_dollars") || ""),
  );
  if (!secret || !cents) return;

  await relay.addPoolKey(label, secret, provider, poolGroup, cents);
  revalidatePath("/pool");
}

export async function topUpPoolKeyAction(id, formData) {
  const cents = centsFromDollarsInput(
    String(formData.get("balance_dollars") || ""),
  );
  if (!cents) return;
  await relay.topUpPoolKey(id, cents);
  revalidatePath("/pool");
}

export async function rotatePoolKeyAction(id, formData) {
  const label = String(formData.get("label") || "").trim();
  const secret = String(formData.get("secret") || "").trim();
  const cents = centsFromDollarsInput(
    String(formData.get("balance_dollars") || ""),
  );
  if (!secret || !cents) return;
  await relay.rotatePoolKey(id, label, secret, cents);
  revalidatePath("/pool");
}

export async function enablePoolKeyAction(id) {
  await relay.enablePoolKey(id);
  revalidatePath("/pool");
}

export async function disablePoolKeyAction(id) {
  await relay.disablePoolKey(id);
  revalidatePath("/pool");
}

export async function deletePoolKeyAction(id) {
  await relay.deletePoolKey(id);
  revalidatePath("/pool");
}

export async function saveEndpointProfileAction(formData) {
  const name = String(formData.get("name") || "").trim();
  const claudeBaseURL = String(formData.get("claude_base_url") || "").trim();
  const poolGroup = String(formData.get("pool_group") || "").trim();
  const active = String(formData.get("active") || "") === "on";
  const billingMode = String(formData.get("billing_mode") || "per_request");
  const perRequestCostCents = centsFromDollarsInput(String(formData.get("per_request_cost_dollars") || "0.30")) || 30;
  const inputCostPerM = centsFromDollarsInput(String(formData.get("input_cost_per_m_dollars") || "0")) || 0;
  const outputCostPerM = centsFromDollarsInput(String(formData.get("output_cost_per_m_dollars") || "0")) || 0;
  if (!name || !claudeBaseURL) return;

  await relay.saveEndpointProfile(name, claudeBaseURL, poolGroup, active, billingMode, perRequestCostCents, inputCostPerM, outputCostPerM);
  revalidatePath("/pool");
}

export async function activateEndpointProfileAction(name) {
  await relay.activateEndpointProfile(name);
  revalidatePath("/pool");
}

export async function deleteEndpointProfileAction(name) {
  await relay.deleteEndpointProfile(name);
  revalidatePath("/pool");
}

export async function getUpstreamUsageAction(id) {
  return await relay.getUpstreamUsage(id);
}
