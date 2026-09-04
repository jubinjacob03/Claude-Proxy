import { cn } from "@/lib/utils";

export default function Brand({
  showText = true,
  width = 160,
  height = 56,
  className,
  textClassName,
}) {
  return (
    <span className={cn("flex items-center gap-3", className)}>
      <span className="relative flex shrink-0 items-center justify-center text-primary" style={{ width: height, height }}>
        <svg
          fill="currentColor"
          viewBox="0 0 24 24"
          xmlns="http://www.w3.org/2000/svg"
          className="size-7"
        >
          <path d="M13.827 3.52h3.603L24 20h-3.603l-6.57-16.48zm-7.258 0h3.767L16.906 20h-3.674l-1.343-3.461H5.017l-1.344 3.46H0L6.57 3.522zm4.132 9.959L8.453 7.687 6.205 13.48H10.7z" />
        </svg>
      </span>
      {showText && (
        <span
          className={cn(
            "font-serif text-2xl tracking-tight text-white/90",
            textClassName,
          )}
        >
          Claude-Proxy
        </span>
      )}
    </span>
  );
}
