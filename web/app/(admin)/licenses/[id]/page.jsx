import Link from "next/link";
import { Cpu, KeyRound, ListChecks } from "lucide-react";
import { relay } from "@/lib/relay";
import { formatCents, formatDateTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import CopyButton from "@/components/CopyButton";

function Stat({ label, value, hint }) {
  return (
    <div className="rounded-xl border border-white/5 bg-black/20 p-4">
      <p className="text-xs uppercase tracking-wider text-neutral-500">
        {label}
      </p>
      <p className="mt-2 text-lg font-semibold text-white">{value}</p>
      {hint ? <p className="mt-1 text-xs text-neutral-500">{hint}</p> : null}
    </div>
  );
}

export default async function LicenseDetailPage({ params }) {
  const { id } = await params;
  const [{ events }, license] = await Promise.all([
    relay.usage(id),
    relay.getLicense(id),
  ]);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-sm text-neutral-500">Licence detail</p>
          <div className="mt-1 flex items-center gap-2">
            <h2 className="font-mono text-lg text-white">{license.key_hint}</h2>
            <CopyButton value={license.key_hint} />
          </div>
        </div>
        <Link
          href="/licenses"
          className="text-sm text-primary/80 transition hover:text-primary/70"
        >
          Back to licences
        </Link>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          label="Status"
          value={license.active ? "Active" : "Paused"}
          hint={license.bound ? "Bound to a machine" : "Not activated yet"}
        />
        <Stat
          label="Spent"
          value={formatCents(license.spent_cents)}
          hint={`of ${formatCents(license.quota_cents)}`}
        />
        <Stat
          label="Remaining"
          value={formatCents(license.remaining_cents)}
          hint={license.note || "No note"}
        />
        <Stat
          label="Last seen"
          value={
            license.last_seen_at ? formatDateTime(license.last_seen_at) : "—"
          }
          hint={
            license.bound_at
              ? `Bound ${formatDateTime(license.bound_at)}`
              : "Never bound"
          }
        />
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
              <KeyRound className="size-5" />
            </div>
            <div>
              <CardTitle>Licence metadata</CardTitle>
              <CardDescription>
                Current state and binding information
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="rounded-xl border border-white/5 bg-black/20 p-4">
            <p className="text-xs uppercase tracking-wider text-neutral-500">
              ID
            </p>
            <p className="mt-2 font-mono text-sm text-white">{license.id}</p>
          </div>
          <div className="rounded-xl border border-white/5 bg-black/20 p-4">
            <p className="text-xs uppercase tracking-wider text-neutral-500">
              Licence key
            </p>
            <div className="mt-2 flex items-center gap-2">
              <p className="font-mono text-sm text-white">{license.key_hint}</p>
              <CopyButton value={license.key_hint} />
            </div>
          </div>
          <div className="rounded-xl border border-white/5 bg-black/20 p-4">
            <p className="text-xs uppercase tracking-wider text-neutral-500">
              Machine
            </p>
            <p className="mt-2 inline-flex items-center gap-2 font-mono text-sm text-white">
              <Cpu className="size-4 text-emerald-300" />
              {license.bound ? license.hwid : "Not activated"}
            </p>
          </div>
          <div className="rounded-xl border border-white/5 bg-black/20 p-4">
            <p className="text-xs uppercase tracking-wider text-neutral-500">
              Created
            </p>
            <p className="mt-2 text-sm text-white">
              {formatDateTime(license.created_at)}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
              <ListChecks className="size-5" />
            </div>
            <div>
              <CardTitle>Usage history</CardTitle>
              <CardDescription>
                Recent metered requests for this licence
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="max-h-[40rem] overflow-auto rounded-xl border border-white/5">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="sticky top-0 z-10 bg-[#1e1e1e]/95 text-xs uppercase tracking-wider text-neutral-500 backdrop-blur">
                <tr className="border-b border-primary/15">
                  <th className="px-3 py-3">Provider</th>
                  <th className="px-3 py-3">Model</th>
                  <th className="px-3 py-3">Cost</th>
                  <th className="px-3 py-3">Status</th>
                  <th className="px-3 py-3">Streamed</th>
                  <th className="px-3 py-3">When</th>
                </tr>
              </thead>
              <tbody>
                {events.map((event) => (
                  <tr key={event.id} className="border-b border-white/5">
                    <td className="px-3 py-3 uppercase text-neutral-400">
                      {event.provider}
                    </td>
                    <td className="px-3 py-3">{event.model}</td>
                    <td className="px-3 py-3">
                      {formatCents(event.cost_cents)}
                    </td>
                    <td className="px-3 py-3">
                      <Badge
                        variant={
                          event.status_code < 400 ? "success" : "destructive"
                        }
                      >
                        {event.status_code}
                      </Badge>
                    </td>
                    <td className="px-3 py-3 text-neutral-400">
                      {event.streamed ? "yes" : "no"}
                    </td>
                    <td className="px-3 py-3 text-neutral-500">
                      {formatDateTime(event.created_at)}
                    </td>
                  </tr>
                ))}
                {events.length === 0 ? (
                  <tr>
                    <td
                      colSpan={6}
                      className="px-3 py-8 text-center text-neutral-500"
                    >
                      No metered requests recorded for this licence yet.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
