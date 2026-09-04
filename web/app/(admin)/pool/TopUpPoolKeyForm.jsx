"use client";

import { useActionState } from "react";
import { topUpPoolKeyAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const initialState = { error: null };

export default function TopUpPoolKeyForm({ id }) {
  const [state, formAction, pending] = useActionState(
    async (_prevState, formData) => {
      try {
        await topUpPoolKeyAction(id, formData);
        return { error: null };
      } catch (error) {
        return { error: error.message || "Top-up failed." };
      }
    },
    initialState,
  );

  return (
    <form action={formAction} className="flex flex-wrap items-center gap-2">
      <Input
        name="balance_dollars"
        type="number"
        min="0.01"
        step="0.01"
        placeholder="Top up $"
        className="h-9 w-24 text-xs"
        required
      />
      <Button type="submit" variant="ghost" size="sm" disabled={pending}>
        {pending ? "Adding…" : "Top up"}
      </Button>
      {state.error ? <p className="text-xs text-red-300">{state.error}</p> : null}
    </form>
  );
}
