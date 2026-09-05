"use client";

import { useActionState, useState } from "react";
import { saveEndpointProfileAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import Link from "next/link";
import { useActionToast } from "@/components/toast/ToastProvider";

const initialState = { error: null };

export default function EndpointProfileForm({ profile }) {
  const [state, formAction, pending] = useActionState(
    async (_prevState, formData) => {
      try {
        await saveEndpointProfileAction(formData);
        return { error: null, success: true };
      } catch (error) {
        if (error.message === "NEXT_REDIRECT") throw error;
        return { error: error.message || "Failed to save endpoint profile." };
      }
    },
    initialState,
  );

  useActionToast(state, profile ? "Base URL updated" : "Base URL created");

  const [billingMode, setBillingMode] = useState(profile?.billing_mode || "per_request");

  return (
    <form key={profile?.name || "new"} action={formAction} className="grid gap-4 sm:grid-cols-5">
      <div className="flex flex-col gap-2">
        <Label htmlFor="profile_name">Profile name</Label>
        <Input id="profile_name" name="name" defaultValue={profile?.name || ""} placeholder="eu" required readOnly={!!profile} className={profile ? "opacity-60 cursor-not-allowed" : ""} />
      </div>
      <div className="flex flex-col gap-2 sm:col-span-2">
        <Label htmlFor="profile_claude_base_url">Claude base URL</Label>
        <Input
          id="profile_claude_base_url"
          name="claude_base_url"
          defaultValue={profile?.claude_base_url || ""}
          placeholder="https://relay-edge.example.com"
          required
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="profile_pool_group">Pool group</Label>
        <Input
          id="profile_pool_group"
          name="pool_group"
          defaultValue={profile?.pool_group || "default"}
          required
        />
      </div>
      <div className="flex items-end gap-2 pb-2">
        <label className="flex items-center gap-2 text-sm text-neutral-300">
          <input type="checkbox" name="active" className="accent-primary" defaultChecked={profile ? profile.active : false} />
          Active
        </label>
      </div>

      <div className="sm:col-span-5 grid gap-4 sm:grid-cols-4 p-4 bg-neutral-900 rounded-lg border border-neutral-800">
        <div className="flex flex-col gap-2">
          <Label htmlFor="billing_mode">Billing mode</Label>
          <select
            id="billing_mode"
            name="billing_mode"
            value={billingMode}
            onChange={(e) => setBillingMode(e.target.value)}
            className="flex h-9 w-full rounded-md border border-neutral-800 bg-neutral-950 px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="per_request">Per Request</option>
            <option value="token_based">Token Based</option>
          </select>
        </div>

        {billingMode === "per_request" ? (
          <div className="flex flex-col gap-2">
            <Label htmlFor="per_request_cost_dollars">Cost per request ($)</Label>
            <Input
              id="per_request_cost_dollars"
              name="per_request_cost_dollars"
              placeholder="0.30"
              defaultValue={profile?.per_request_cost_cents ? (profile.per_request_cost_cents / 100).toFixed(2) : "0.30"}
              required
            />
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-2">
              <Label htmlFor="input_cost_per_m_dollars">Input cost / 1M ($)</Label>
              <Input
                id="input_cost_per_m_dollars"
                name="input_cost_per_m_dollars"
                placeholder="15.00"
                defaultValue={profile?.input_cost_per_m ? (profile.input_cost_per_m / 100).toFixed(2) : "15.00"}
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="output_cost_per_m_dollars">Output cost / 1M ($)</Label>
              <Input
                id="output_cost_per_m_dollars"
                name="output_cost_per_m_dollars"
                placeholder="75.00"
                defaultValue={profile?.output_cost_per_m ? (profile.output_cost_per_m / 100).toFixed(2) : "75.00"}
                required
              />
            </div>
          </>
        )}
      </div>

      <div className="sm:col-span-5 flex items-center gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving..." : (profile ? "Update Base URL" : "Save Base URL")}
        </Button>
        {profile && (
          <Button type="button" variant="ghost" asChild>
            <Link href="/pool" scroll={false}>Cancel Edit</Link>
          </Button>
        )}
        {state.error ? (
          <p className="mt-2 text-sm text-red-300">{state.error}</p>
        ) : null}
      </div>
    </form>
  );
}
