import { Activity, KeyRound, ShieldCheck, Wallet } from "lucide-react";
import { relay } from "@/lib/relay";
import { formatCents } from "@/lib/format";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

function Stat({ icon: Icon, label, value, hint }) {
  return (
    <Card>
      <CardContent className="flex items-start gap-4 pt-6">
        <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
          <Icon className="size-5" />
        </div>
        <div>
          <p className="text-2xl font-semibold text-white">{value}</p>
          <p className="text-sm text-neutral-400">{label}</p>
          {hint ? (
            <p className="mt-1 text-xs text-neutral-500">{hint}</p>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}

export default async function DashboardPage() {
  const [{ licenses }, { pool }] = await Promise.all([
    relay.listLicenses(),
    relay.listPool(),
  ]);

  const active = licenses.filter((l) => l.active).length;
  const bound = licenses.filter((l) => l.bound).length;
  const totalSpent = licenses.reduce((sum, l) => sum + l.spent_cents, 0);
  const totalQuota = licenses.reduce((sum, l) => sum + l.quota_cents, 0);
  const poolRemaining = pool.reduce((sum, k) => sum + k.remaining_cents, 0);

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          icon={KeyRound}
          label="Licences"
          value={licenses.length}
          hint={`${active} active`}
        />
        <Stat
          icon={ShieldCheck}
          label="Activated machines"
          value={bound}
          hint="bound to a device"
        />
        <Stat
          icon={Activity}
          label="Spend so far"
          value={formatCents(totalSpent)}
          hint={`of ${formatCents(totalQuota)} issued`}
        />
        <Stat
          icon={Wallet}
          label="Pool credit left"
          value={formatCents(poolRemaining)}
          hint={`${pool.filter((k) => k.active).length} active keys`}
        />
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-3xl font-normal text-white/90">Getting started</CardTitle>
          <CardDescription>
            Add a pooled upstream key under Key pool, then generate licences under
            Licences. Each licence binds to one machine on first use and stops
            working once its balance is spent.
          </CardDescription>
        </CardHeader>
      </Card>
    </div>
  );
}
