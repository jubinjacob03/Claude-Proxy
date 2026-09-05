"use client";

import { useActionState } from "react";
import { ChevronDown } from "lucide-react";
import { addPoolKeyAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useActionToast } from "@/components/toast/ToastProvider";

const initialState = { error: null };

async function submit(_prevState, formData) {
  try {
    await addPoolKeyAction(formData);
    return { error: null, success: true };
  } catch (err) {
    return { error: err.message || "Failed to add the key." };
  }
}

export default function AddPoolKeyForm({ poolGroups = [] }) {
  const [state, formAction, pending] = useActionState(submit, initialState);
  useActionToast(state, "Key added to pool");

  return (
    <form action={formAction} className="grid gap-4 sm:grid-cols-4">
      <div className="flex flex-col gap-2 sm:col-span-2">
        <Label htmlFor="secret">Upstream API key</Label>
        <Input
          id="secret"
          name="secret"
          type="password"
          autoComplete="off"
          required
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="balance_dollars">Balance (USD)</Label>
        <Input
          id="balance_dollars"
          name="balance_dollars"
          type="number"
          min="1"
          step="0.01"
          required
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="label">Label</Label>
        <Input id="label" name="label" placeholder="optional" />
      </div>
      {poolGroups.length > 0 && (
        <div className="flex flex-col gap-2">
          <Label htmlFor="pool_group_key">Pool group</Label>
          <div className="relative">
            <select
              id="pool_group_key"
              name="pool_group"
              defaultValue={poolGroups[0]}
              className="appearance-none w-full h-11 rounded-xl border border-primary/15 bg-black/40 pl-3 pr-8 text-sm text-white outline-none focus:border-primary/70"
            >
              {poolGroups.map((group) => (
                <option key={group} value={group}>
                  {group}
                </option>
              ))}
            </select>
            <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-neutral-500" />
          </div>
        </div>
      )}
      <div className="sm:col-span-4">
        <Button type="submit" disabled={pending}>
          {pending ? "Adding..." : "Add key to pool"}
        </Button>
        {state.error ? (
          <p className="mt-2 text-sm text-red-300">{state.error}</p>
        ) : null}
      </div>
    </form>
  );
}
