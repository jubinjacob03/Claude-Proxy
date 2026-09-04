"use client";

import { useActionState } from "react";
import { rotatePoolKeyAction } from "../actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const initialState = { error: null };

export default function RotatePoolKeyForm({ id, defaultLabel }) {
  const [state, formAction, pending] = useActionState(
    async (_prevState, formData) => {
      try {
        await rotatePoolKeyAction(id, formData);
        return { error: null };
      } catch (error) {
        return { error: error.message || "Rotation failed." };
      }
    },
    initialState,
  );

  return (
    <form action={formAction} className="flex flex-wrap items-center gap-2">
      <Input
        name="label"
        defaultValue={defaultLabel}
        placeholder="Label"
        className="h-9 w-28 text-xs"
      />
      <Input
        name="secret"
        type="password"
        placeholder="New key"
        className="h-9 w-40 text-xs"
        required
      />
      <Input
        name="balance_dollars"
        type="number"
        min="0.01"
        step="0.01"
        placeholder="New balance $"
        className="h-9 w-28 text-xs"
        required
      />
      <Button type="submit" variant="outline" size="sm" disabled={pending}>
        {pending ? "Rotating…" : "Rotate"}
      </Button>
      {state.error ? <p className="text-xs text-red-300">{state.error}</p> : null}
    </form>
  );
}
