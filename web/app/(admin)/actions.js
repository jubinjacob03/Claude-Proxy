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
  return { error: null, success: true, created };
}

export async function pauseLicenseAction(id) {
  try {
    await relay.pauseLicense(id);
    revalidatePath("/licenses");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to pause license." };
  }
}

export async function resumeLicenseAction(id) {
  try {
    await relay.resumeLicense(id);
    revalidatePath("/licenses");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to resume license." };
  }
}

export async function resetHwidAction(id) {
  try {
    await relay.resetHwid(id);
    revalidatePath("/licenses");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to reset HWID." };
  }
}

export async function setQuotaAction(id, formData) {
  const cents = centsFromDollarsInput(
    String(formData.get("quota_dollars") || ""),
  );
  if (!cents) return;
  try {
    await relay.setQuota(id, cents);
    revalidatePath("/licenses");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to set quota." };
  }
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
  try {
    await relay.enablePoolKey(id);
    revalidatePath("/pool");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to enable key." };
  }
}

export async function disablePoolKeyAction(id) {
  try {
    await relay.disablePoolKey(id);
    revalidatePath("/pool");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to disable key." };
  }
}

export async function deletePoolKeyAction(id) {
  try {
    await relay.deletePoolKey(id);
    revalidatePath("/pool");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to delete key." };
  }
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
  redirect("/pool");
}

export async function activateEndpointProfileAction(name) {
  try {
    await relay.activateEndpointProfile(name);
    revalidatePath("/pool");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to activate profile." };
  }
}

export async function deleteEndpointProfileAction(name) {
  try {
    await relay.deleteEndpointProfile(name);
    revalidatePath("/pool");
    return { success: true, error: null };
  } catch (err) {
    if (err.message === "NEXT_REDIRECT") throw err;
    return { error: err.message || "Failed to delete profile." };
  }
}

export async function getUpstreamUsageAction(id) {
  return await relay.getUpstreamUsage(id);
}
