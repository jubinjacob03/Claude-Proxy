"use client";

import { Button } from "@/components/ui/button";

export default function ConfirmButton({ message, children, ...props }) {
  return (
    <Button
      {...props}
      onClick={(event) => {
        if (!window.confirm(message)) {
          event.preventDefault();
        }
      }}
    >
      {children}
    </Button>
  );
}
