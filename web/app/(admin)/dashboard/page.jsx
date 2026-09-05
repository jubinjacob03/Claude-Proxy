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
import MotionCard from "@/components/motion/MotionCard";

function Stat({ icon: Icon, label, value, hint, delay = 0 }) {
  return (
    <MotionCard delay={delay}>
      <Card className="h-full">
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
    </MotionCard>
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
          delay={0.1}
        />
        <Stat
          icon={ShieldCheck}
          label="Activated machines"
          value={bound}
          hint="bound to a device"
          delay={0.2}
        />
        <Stat
          icon={Activity}
          label="Spend so far"
          value={formatCents(totalSpent)}
          hint={`of ${formatCents(totalQuota)} issued`}
          delay={0.3}
        />
        <Stat
          icon={Wallet}
          label="Pool credit left"
          value={formatCents(poolRemaining)}
          hint={`${pool.filter((k) => k.active).length} active keys`}
          delay={0.4}
        />
      </div>
      <MotionCard delay={0.5}>
        <Card>
          <CardHeader>
            <CardTitle className="font-serif text-3xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">
              Getting started
            </CardTitle>
          <CardDescription>
            Add a pooled upstream key under Key pool, then generate licences under
            Licences. Each licence binds to one machine on first use and stops
            working once its balance is spent.
          </CardDescription>
        </CardHeader>
        </Card>
      </MotionCard>
    </div>
  );
}
