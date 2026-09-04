"use client";

import { useRef } from "react";
import { cn } from "@/lib/utils";

function Card({ className, children, ...props }) {
  const ref = useRef(null);
  const frame = useRef(0);

  function handlePointerMove(event) {
    const element = ref.current;
    if (!element) return;
    const { clientX, clientY } = event;
    if (frame.current) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = 0;
      const rect = element.getBoundingClientRect();
      element.style.setProperty("--mx", `${clientX - rect.left}px`);
      element.style.setProperty("--my", `${clientY - rect.top}px`);
    });
  }

  return (
    <section
      ref={ref}
      onPointerMove={handlePointerMove}
      className={cn("surface-card rounded-2xl", className)}
      {...props}
    >
      <span aria-hidden className="card-glow" />
      {children}
    </section>
  );
}

function CardHeader({ className, ...props }) {
  return (
    <div className={cn("flex flex-col gap-1.5 p-6", className)} {...props} />
  );
}

function CardTitle({ className, ...props }) {
  return (
    <h2
      className={cn("font-semibold tracking-tight text-white", className)}
      {...props}
    />
  );
}

function CardDescription({ className, ...props }) {
  return (
    <p
      className={cn("text-sm leading-6 text-neutral-400", className)}
      {...props}
    />
  );
}

function CardContent({ className, ...props }) {
  return <div className={cn("px-6 pb-6", className)} {...props} />;
}

export { Card, CardHeader, CardTitle, CardDescription, CardContent };
