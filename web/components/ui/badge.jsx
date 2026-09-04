import { cva } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold tracking-wide transition-colors",
  {
    variants: {
      variant: {
        default: "border-primary/30 bg-primary/12 text-primary/80",
        success: "border-emerald-400/30 bg-emerald-500/12 text-emerald-200",
        warning: "border-amber-400/30 bg-amber-500/12 text-amber-100",
        destructive: "border-primary/40 bg-primary/15 text-primary/80",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

function Badge({ className, variant, ...props }) {
  return (
    <span className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}

export { Badge };
