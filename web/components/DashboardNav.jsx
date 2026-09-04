"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { motion } from "motion/react";
import {
  KeyRound,
  LayoutDashboard,
  ListChecks,
  Wallet,
} from "lucide-react";
import { cn } from "@/lib/utils";

const links = [
  ["Dashboard", "/dashboard", LayoutDashboard],
  ["Licences", "/licenses", KeyRound],
  ["Key pool", "/pool", Wallet],
  ["Usage", "/usage", ListChecks],
];

function isActive(pathname, href) {
  return (
    pathname === href || (href !== "/dashboard" && pathname.startsWith(href))
  );
}

export function SidebarNav() {
  const pathname = usePathname();
  return (
    <nav className="space-y-1.5">
      {links.map(([label, href, Icon]) => {
        const active = isActive(pathname, href);
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "group relative flex items-center gap-3 rounded-full px-4 py-2.5 text-sm font-medium transition-[color,transform] duration-200 ease-[cubic-bezier(.2,.8,.2,1)]",
              active
                ? "text-primary-foreground"
                : "text-neutral-400 hover:translate-x-0.5 hover:text-white",
            )}
          >
            {active && (
              <motion.span
                layoutId="sidebar-active"
                transition={{ type: "spring", stiffness: 420, damping: 34 }}
                className="absolute inset-0 -z-10 rounded-full border border-white/5 bg-primary/20 backdrop-blur-sm"
              />
            )}
            <Icon
              className={cn(
                "size-4 transition-transform duration-200 group-hover:scale-110",
                active && "text-primary",
              )}
            />
            {label}
          </Link>
        );
      })}
    </nav>
  );
}

export function MobileNav() {
  const pathname = usePathname();
  return (
    <>
      {links.map(([label, href, Icon]) => {
        const active = isActive(pathname, href);
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "relative inline-flex shrink-0 items-center gap-2 rounded-full px-4 py-2 text-sm transition-colors",
              active ? "text-primary-foreground" : "text-neutral-400 hover:text-white",
            )}
          >
            {active && (
              <motion.span
                layoutId="mobile-active"
                transition={{ type: "spring", stiffness: 420, damping: 34 }}
                className="absolute inset-0 -z-10 rounded-full border border-white/5 bg-primary/20 backdrop-blur-sm"
              />
            )}
            <Icon className="size-4" />
            {label}
          </Link>
        );
      })}
    </>
  );
}
