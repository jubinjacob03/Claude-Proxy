import { Cpu, KeyRound, ShieldOff } from "lucide-react";
import { relay } from "@/lib/relay";
import { formatCents, formatDateTime } from "@/lib/format";
import {
  pauseLicenseAction,
  resetHwidAction,
  resumeLicenseAction,
  setQuotaAction,
} from "../actions";
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
import MintForm from "./MintForm";
import CopyButton from "@/components/CopyButton";
import MotionRow from "@/components/motion/MotionRow";
import { ActionForm } from "@/components/ActionForm";

function HwidCell({ bound, hwid }) {
  if (!bound) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-neutral-500">
        <ShieldOff className="size-3.5" />
        Not activated
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-xs text-emerald-300">
      <Cpu className="size-3.5" />
      {hwid}
    </span>
  );
}

export default async function LicensesPage() {
  const { licenses } = await relay.listLicenses();

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
              <KeyRound className="size-5" />
            </div>
            <div>
              <CardTitle className="font-serif text-3xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">Generate licences</CardTitle>
              <CardDescription>
                The full key is shown below and can be copied at any time from the table.
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <MintForm />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-2xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">All licences</CardTitle>
          <CardDescription>{licenses.length} issued</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="max-h-[36rem] overflow-auto rounded-xl border border-white/5">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="sticky top-0 z-10 bg-[#1e1e1e]/95 text-xs uppercase tracking-wider text-neutral-500 backdrop-blur">
                <tr className="border-b border-primary/15">
                  <th className="px-3 py-3">Key</th>
                  <th className="px-3 py-3">Status</th>
                  <th className="px-3 py-3">Machine</th>
                  <th className="px-3 py-3">Usage</th>
                  <th className="px-3 py-3">Note</th>
                  <th className="px-3 py-3">Created</th>
                  <th className="px-3 py-3">Actions</th>
                </tr>
              </thead>
              <tbody>
                {licenses.map((l) => (
                  <MotionRow key={l.id} className="border-b border-white/5 transition-colors hover:bg-white/[0.02]">
                    <td className="px-3 py-3 font-mono text-xs">
                      <div className="flex items-center gap-2">
                        {l.raw_key ? l.raw_key : l.key_hint}
                        <CopyButton value={l.raw_key || l.key_hint} />
                      </div>
                    </td>
                    <td className="px-3 py-3">
                      <Badge variant={l.active ? "success" : "destructive"}>
                        {l.active ? "Active" : "Paused"}
                      </Badge>
                    </td>
                    <td className="px-3 py-3">
                      <HwidCell bound={l.bound} hwid={l.hwid} />
                    </td>
                    <td className="px-3 py-3">
                      {formatCents(l.spent_cents)} of{" "}
                      {formatCents(l.quota_cents)}
                    </td>
                    <td className="px-3 py-3 text-neutral-400">
                      {l.note || "—"}
                    </td>
                    <td className="px-3 py-3 text-neutral-500">
                      {formatDateTime(l.created_at)}
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex flex-wrap gap-2">
                        <ActionForm
                          action={
                            l.active
                              ? pauseLicenseAction.bind(null, l.id)
                              : resumeLicenseAction.bind(null, l.id)
                          }
                          successMessage={l.active ? "License paused" : "License resumed"}
                        >
                          <Button type="submit" variant="secondary" size="sm">
                            {l.active ? "Pause" : "Resume"}
                          </Button>
                        </ActionForm>
                        <ActionForm
                          action={resetHwidAction.bind(null, l.id)}
                          successMessage="HWID reset"
                        >
                          <Button type="submit" variant="outline" size="sm">
                            Reset HWID
                          </Button>
                        </ActionForm>
                        <ActionForm
                          action={setQuotaAction.bind(null, l.id)}
                          successMessage="Quota set"
                          className="flex items-center gap-1.5"
                        >
                          <Input
                            name="quota_dollars"
                            type="number"
                            min="1"
                            step="0.01"
                            placeholder="Top up $"
                            className="h-9 w-28 text-xs"
                          />
                          <Button type="submit" variant="ghost" size="sm">
                            Set
                          </Button>
                        </ActionForm>
                      </div>
                    </td>
                  </MotionRow>
                ))}
                {licenses.length === 0 ? (
                  <tr>
                    <td
                      colSpan={7}
                      className="px-3 py-8 text-center text-neutral-500"
                    >
                      No licences yet. Generate one above.
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
