"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { formatCents } from "@/lib/format";

export function UpstreamSpendButton({ id, getUsage }) {
  const [loading, setLoading] = useState(false);
  const [spendCents, setSpendCents] = useState(null);
  const [error, setError] = useState(null);

  const fetchSpend = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await getUsage(id);
      if (res && res.total_usage !== undefined) {
        setSpendCents(res.total_usage);
      } else {
        throw new Error("Invalid response format");
      }
    } catch (err) {
      setError(err.message || "Failed to fetch");
    } finally {
      setLoading(false);
    }
  };

  if (spendCents !== null) {
    return <span className="text-sm font-mono text-primary">{formatCents(spendCents)}</span>;
  }

  return (
    <div className="flex items-center gap-2">
      <Button variant="outline" size="sm" onClick={fetchSpend} disabled={loading}>
        {loading ? "Loading..." : "Check Spend"}
      </Button>
      {error && <span className="text-xs text-red-400">{error}</span>}
    </div>
  );
}
