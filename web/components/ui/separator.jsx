import { cn } from "@/lib/utils";

function Separator({ className, ...props }) {
  return (
    <div
      className={cn(
        "h-px w-full bg-gradient-to-r from-transparent via-primary/25 to-transparent",
        className,
      )}
      {...props}
    />
  );
}

export { Separator };
