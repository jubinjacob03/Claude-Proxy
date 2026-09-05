"use client";

import { useActionState } from "react";
import { useActionToast } from "./toast/ToastProvider";

export function ActionForm({ action, successMessage, children, className }) {
  const [state, formAction] = useActionState(
    async (prevState, formData) => {
      try {
        await action(formData);
        return { success: true, error: null };
      } catch (err) {
        if (err.message === "NEXT_REDIRECT") throw err;
        return { error: err.message || "Action failed." };
      }
    },
    { error: null }
  );

  useActionToast(state, successMessage);

  return (
    <form action={formAction} className={className}>
      {children}
    </form>
  );
}
