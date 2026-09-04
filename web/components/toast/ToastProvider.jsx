"use client";

import { AnimatePresence, motion } from "motion/react";
import { CircleCheck, TriangleAlert, X } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
} from "react";
import { cn } from "@/lib/utils";

const ToastContext = createContext(null);

export function useToast() {
  const context = useContext(ToastContext);
  return context?.toast ?? (() => {});
}

export default function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);
  const counter = useRef(0);

  const dismiss = useCallback(
    (id) => setToasts((list) => list.filter((toast) => toast.id !== id)),
    [],
  );

  const toast = useCallback(
    (message, variant = "success") => {
      if (!message) return;
      const id = (counter.current += 1);
      setToasts((list) => [...list, { id, message, variant }]);
      setTimeout(() => dismiss(id), 4200);
    },
    [dismiss],
  );

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[200] flex w-full max-w-sm flex-col gap-2">
        <AnimatePresence initial={false}>
          {toasts.map((item) => (
            <motion.div
              key={item.id}
              layout
              initial={{ opacity: 0, y: 16, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, x: 48, scale: 0.96 }}
              transition={{ type: "spring", stiffness: 320, damping: 28 }}
              className={cn(
                "pointer-events-auto flex items-start gap-3 rounded-xl border px-4 py-3 shadow-[0_20px_50px_-20px_rgba(0,0,0,.85)] backdrop-blur-xl",
                item.variant === "error"
                  ? "border-primary/40 bg-[#1a0b0e]/95 text-primary/90"
                  : "border-emerald-400/30 bg-[#0b1512]/95 text-emerald-100",
              )}
            >
              {item.variant === "error" ? (
                <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              ) : (
                <CircleCheck className="mt-0.5 size-4 shrink-0" />
              )}
              <p className="min-w-0 flex-1 text-sm leading-6">{item.message}</p>
              <button
                type="button"
                onClick={() => dismiss(item.id)}
                aria-label="Dismiss"
                className="shrink-0 text-neutral-400 transition-colors hover:text-white"
              >
                <X className="size-4" />
              </button>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  );
}
