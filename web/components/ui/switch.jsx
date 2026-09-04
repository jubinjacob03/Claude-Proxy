"use client";

import { motion } from "motion/react";
import { cn } from "@/lib/utils";

export function Switch({ checked, onCheckedChange, id }) {
  return (
    <button
      type="button"
      role="switch"
      id={id}
      aria-checked={checked}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        "relative inline-flex h-6 w-11 items-center rounded-full border transition-colors duration-200",
        checked
          ? "border-primary/50 bg-primary/80"
          : "border-white/15 bg-white/5",
      )}
    >
      <motion.span
        layout
        transition={{ type: "spring", stiffness: 500, damping: 32 }}
        className={cn(
          "mx-0.5 size-4 rounded-full bg-white shadow",
          checked ? "ml-auto" : "mr-auto",
        )}
      />
    </button>
  );
}
