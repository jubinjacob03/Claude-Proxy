"use client";

import { useActionState } from "react";
import { mintLicenseAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import CopyButton from "@/components/CopyButton";

const initialState = { error: null, created: null };

export default function MintForm() {
  const [state, formAction, pending] = useActionState(
    mintLicenseAction,
    initialState,
  );

  return (
    <div className="flex flex-col gap-4">
      <form action={formAction} className="grid gap-4 sm:grid-cols-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="quota_dollars">Quota (USD)</Label>
          <Input
            id="quota_dollars"
            name="quota_dollars"
            type="number"
            min="1"
            step="0.01"
            defaultValue="70"
            required
          />
        </div>
        <div className="flex flex-col gap-2 sm:col-span-2">
          <Label htmlFor="note">Note</Label>
          <Input id="note" name="note" placeholder="e.g. customer email" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="count">How many</Label>
          <Input
            id="count"
            name="count"
            type="number"
            min="1"
            max="500"
            defaultValue="1"
          />
        </div>
        <div className="sm:col-span-4">
          <Button type="submit" disabled={pending}>
            {pending ? "Generating…" : "Generate licence"}
          </Button>
        </div>
      </form>

      {state.error ? (
        <p className="text-sm text-red-300">{state.error}</p>
      ) : null}

      {state.created?.length ? (
        <div className="rounded-xl border border-amber-400/30 bg-amber-500/10 p-4">
          <p className="mb-3 text-sm font-semibold text-amber-100">
            Successfully generated! You can copy them below, or anytime from the table:
          </p>
          <div className="flex flex-col gap-2">
            {state.created.map((k) => (
              <div
                key={k.id}
                className="flex items-center justify-between gap-3 rounded-lg bg-black/30 px-3 py-2"
              >
                <code className="text-xs text-white">{k.key}</code>
                <CopyButton value={k.key} />
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
