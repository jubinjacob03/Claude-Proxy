import Link from "next/link";
import { ListChecks, ChevronDown } from "lucide-react";
import { relay } from "@/lib/relay";
import { formatCents, formatDateTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import MotionRow from "@/components/motion/MotionRow";

export default async function UsagePage({ searchParams }) {
  const params = await searchParams;
  const q = typeof params?.q === "string" ? params.q : "";
  const status = typeof params?.status === "string" ? params.status : "";
  const licenseId = typeof params?.license_id === "string" ? params.license_id : "";
  const [{ events }, { licenses }] = await Promise.all([
    relay.usage({ license_id: licenseId, q, status }),
    relay.listLicenses(),
  ]);

  const licensesById = new Map(licenses.map((license) => [license.id, license]));
  const exportUrl = `/api/admin/usage/export?license_id=${encodeURIComponent(licenseId)}&q=${encodeURIComponent(q)}&status=${encodeURIComponent(status)}`;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
              <ListChecks className="size-5" />
            </div>
            <div>
              <CardTitle className="font-serif text-2xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">Recent usage</CardTitle>
              <CardDescription>
                {events.length} matching metered requests
              </CardDescription>
            </div>
          </div>
          <Button asChild variant="outline" size="sm">
            <a href={exportUrl}>Export CSV</a>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <form className="flex flex-wrap items-end gap-3">
          <div className="flex min-w-64 flex-1 flex-col gap-2">
            <label className="text-xs uppercase tracking-wide text-neutral-500">
              Search
            </label>
            <Input
              name="q"
              defaultValue={q}
              placeholder="Provider, model, licence ID, pool key ID"
            />
          </div>
          <div className="flex min-w-52 flex-col gap-2">
            <label className="text-xs uppercase tracking-wide text-neutral-500">
              Licence
            </label>
          <div className="relative">
            <select
              name="license_id"
              defaultValue={licenseId}
              className="appearance-none w-full h-10 rounded-md border border-white/10 bg-black/30 pl-3 pr-8 text-sm text-white outline-none"
            >
              <option value="">All</option>
              {licenses.map((license) => (
                <option key={license.id} value={license.id}>
                  {license.key_hint}
                </option>
              ))}
            </select>
            <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-neutral-500" />
          </div>
          </div>
          <div className="flex min-w-44 flex-col gap-2">
            <label className="text-xs uppercase tracking-wide text-neutral-500">
              Status
            </label>
            <div className="relative">
              <select
                name="status"
                defaultValue={status}
                className="appearance-none w-full h-10 rounded-md border border-white/10 bg-black/30 pl-3 pr-8 text-sm text-white outline-none"
              >
                <option value="">All</option>
                <option value="success">Success</option>
                <option value="error">Error</option>
                <option value="streamed">Streamed</option>
                <option value="non-streamed">Non-streamed</option>
              </select>
              <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-neutral-500" />
            </div>
          </div>
          <Button type="submit" size="sm">
            Apply
          </Button>
        </form>

        <div className="max-h-[40rem] overflow-auto rounded-xl border border-white/5">
          <table className="w-full min-w-[860px] text-left text-sm">
            <thead className="sticky top-0 z-10 bg-[#1e1e1e]/95 text-xs uppercase tracking-wider text-neutral-500 backdrop-blur">
              <tr className="border-b border-primary/15">
                <th className="px-3 py-3">Licence</th>
                <th className="px-3 py-3">Provider</th>
                <th className="px-3 py-3">Model</th>
                <th className="px-3 py-3">Cost</th>
                <th className="px-3 py-3">Tokens (in/out)</th>
                <th className="px-3 py-3">Status</th>
                <th className="px-3 py-3">Streamed</th>
                <th className="px-3 py-3">When</th>
              </tr>
            </thead>
            <tbody>
              {events.map((event) => {
                const license = licensesById.get(event.license_id);
                return (
                  <MotionRow key={event.id} className="border-b border-white/5 transition-colors hover:bg-white/[0.02]">
                    <td className="px-3 py-3 font-mono text-xs">
                      {license ? (
                        <Link
                          href={`/licenses/${license.id}`}
                          className="text-primary/70 transition hover:text-white"
                        >
                          {license.key_hint}
                        </Link>
                      ) : (
                        event.license_id.slice(0, 12)
                      )}
                    </td>
                    <td className="px-3 py-3 uppercase text-neutral-400">
                      {event.provider}
                    </td>
                    <td className="px-3 py-3">{event.model}</td>
                    <td className="px-3 py-3">{formatCents(event.cost_cents)}</td>
                    <td className="px-3 py-3 text-xs text-neutral-400">
                      {event.input_tokens || 0} / {event.output_tokens || 0}
                    </td>
                    <td className="px-3 py-3">
                      <Badge
                        variant={event.status_code < 400 ? "success" : "destructive"}
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
                  </MotionRow>
                );
              })}
              {events.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-10 text-center text-neutral-500">
                    <div className="flex flex-col items-center gap-2">
                      <ListChecks className="size-6 opacity-40" />
                      <p>No requests match the current filters.</p>
                    </div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}
