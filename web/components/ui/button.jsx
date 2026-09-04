"use client";

import { Slot } from "@radix-ui/react-slot";
import { cva } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "relative inline-flex items-center justify-center gap-2 overflow-hidden whitespace-nowrap rounded-xl text-sm font-semibold tracking-wide transition-[transform,box-shadow,background-color,border-color] duration-200 ease-[cubic-bezier(.2,.8,.2,1)] will-change-transform active:scale-[.97] disabled:pointer-events-none disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2 focus-visible:ring-offset-[#080405] before:pointer-events-none before:absolute before:inset-0 before:-translate-x-full before:bg-[linear-gradient(110deg,transparent,rgba(255,255,255,.3),transparent)] before:mix-blend-plus-lighter before:transition-transform before:duration-700 before:ease-[cubic-bezier(.2,.8,.2,1)] before:content-[''] hover:before:translate-x-full",
  {
    variants: {
      variant: {
        default:
          "bg-gradient-to-b from-primary to-primary/85 text-white shadow-[0_8px_24px_-8px_rgba(217,119,87,.55)] hover:-translate-y-0.5 hover:shadow-[0_14px_32px_-8px_rgba(217,119,87,.7)]",
        secondary:
          "border border-primary/25 bg-white/[0.03] text-neutral-100 hover:-translate-y-0.5 hover:border-primary/45 hover:bg-white/[0.06]",
        outline:
          "border border-primary/35 bg-transparent text-primary/80 hover:bg-primary/10 hover:border-primary/60",
        destructive:
          "border border-primary/35 bg-primary/10 text-primary/80 hover:bg-primary/20 hover:border-primary/55",
        ghost: "text-neutral-400 hover:bg-white/5 hover:text-white",
      },
      size: {
        default: "h-11 px-5",
        sm: "h-9 rounded-lg px-3.5 text-xs",
        lg: "h-12 px-7 text-base",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

function launchRipple(event) {
  const target = event.currentTarget;
  const diameter = Math.max(target.clientWidth, target.clientHeight);
  const rect = target.getBoundingClientRect();
  const circle = document.createElement("span");
  circle.className = "ripple";
  circle.style.width = circle.style.height = `${diameter}px`;
  circle.style.left = `${event.clientX - rect.left - diameter / 2}px`;
  circle.style.top = `${event.clientY - rect.top - diameter / 2}px`;
  target.appendChild(circle);
  circle.addEventListener("animationend", () => circle.remove());
}

function Button({
  className,
  variant,
  size,
  asChild = false,
  onClick,
  ...props
}) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      className={cn(buttonVariants({ variant, size }), className)}
      onClick={(event) => {
        launchRipple(event);
        onClick?.(event);
      }}
      {...props}
    />
  );
}

export { Button, buttonVariants };
