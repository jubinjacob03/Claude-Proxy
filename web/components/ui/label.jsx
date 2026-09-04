import { cn } from "@/lib/utils";

function Label({ className, ...props }) {
  return (
    <label
      className={cn(
        "text-xs font-semibold uppercase tracking-wider text-neutral-400",
        className,
      )}
      {...props}
    />
  );
}

export { Label };
