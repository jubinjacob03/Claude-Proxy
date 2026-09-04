import { cn } from "@/lib/utils";

function Input({ className, type, ...props }) {
  return (
    <input
      type={type}
      className={cn(
        "flex h-11 w-full rounded-xl border border-primary/15 bg-black/40 px-3.5 py-2 text-sm text-white shadow-inner shadow-black/40 outline-none transition-[border-color,box-shadow,background-color] duration-200 ease-[cubic-bezier(.2,.8,.2,1)] placeholder:text-neutral-500 hover:border-primary/30 focus:border-primary/70 focus:bg-black/60 focus:ring-4 focus:ring-primary/15 disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
