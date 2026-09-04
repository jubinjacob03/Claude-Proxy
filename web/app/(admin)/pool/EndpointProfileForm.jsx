"use client";

import { useActionState } from "react";
import { saveEndpointProfileAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const initialState = { error: null };

export default function EndpointProfileForm() {
  const [state, formAction, pending] = useActionState(
    async (_prevState, formData) => {
      try {
        await saveEndpointProfileAction(formData);
        return { error: null };
      } catch (error) {
        return { error: error.message || "Failed to save endpoint profile." };
      }
    },
    initialState,
  );

  return (
    <form action={formAction} className="grid gap-4 sm:grid-cols-5">
      <div className="flex flex-col gap-2">
        <Label htmlFor="profile_name">Profile name</Label>
        <Input id="profile_name" name="name" placeholder="eu" required />
      </div>
      <div className="flex flex-col gap-2 sm:col-span-2">
        <Label htmlFor="profile_claude_base_url">Claude base URL</Label>
        <Input
          id="profile_claude_base_url"
          name="claude_base_url"
          placeholder="https://relay-edge.example.com"
          required
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="profile_pool_group">Pool group</Label>
        <Input
          id="profile_pool_group"
          name="pool_group"
          defaultValue="default"
          required
        />
      </div>
      <div className="flex items-end gap-2">
        <label className="flex items-center gap-2 text-sm text-neutral-300">
          <input type="checkbox" name="active" className="accent-primary" />
          Active
        </label>
      </div>
      <div className="sm:col-span-5">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving..." : "Save Base URL"}
        </Button>
        {state.error ? (
          <p className="mt-2 text-sm text-red-300">{state.error}</p>
        ) : null}
      </div>
    </form>
  );
}
