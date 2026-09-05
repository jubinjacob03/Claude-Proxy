import { Wallet, ServerOff, KeyRound } from "lucide-react";
import Link from "next/link";
import { relay } from "@/lib/relay";
import { formatCents, formatDateTime } from "@/lib/format";
import {
  activateEndpointProfileAction,
  deleteEndpointProfileAction,
  deletePoolKeyAction,
  disablePoolKeyAction,
  enablePoolKeyAction,
  rotatePoolKeyAction,
  topUpPoolKeyAction,
  getUpstreamUsageAction,
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
import AddPoolKeyForm from "./AddPoolKeyForm";
import EndpointProfileForm from "./EndpointProfileForm";
import ConfirmButton from "./ConfirmButton";
import RotatePoolKeyForm from "./RotatePoolKeyForm";
import TopUpPoolKeyForm from "./TopUpPoolKeyForm";
import MotionRow from "@/components/motion/MotionRow";
import { UpstreamSpendButton } from "./UpstreamSpendButton";

export default async function PoolPage(props) {
  const searchParams = await props.searchParams || {};
  const [{ pool }, { profiles }] = await Promise.all([
    relay.listPool(),
    relay.listEndpointProfiles(),
  ]);
  const editProfile = profiles.find((p) => p.name === searchParams.edit) || null;
  const activeProfile = profiles.find((profile) => profile.active) || null;
  const activePoolGroup = activeProfile?.pool_group || "default";
  const poolGroups = [
    ...new Set([
      ...profiles.map((profile) => profile.pool_group),
      ...pool.map((key) => key.pool_group),
      activePoolGroup,
    ]),
  ]
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b));
  const keysByGroup = new Map();
  for (const group of poolGroups) keysByGroup.set(group, []);
  for (const key of pool) {
    const group = key.pool_group || "default";
    if (!keysByGroup.has(group)) keysByGroup.set(group, []);
    keysByGroup.get(group).push(key);
  }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="glow-ring rounded-xl bg-primary/10 p-2.5 text-primary">
              <Wallet className="size-5" />
            </div>
            <div>
              <CardTitle className="font-serif text-3xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">Base URL profiles</CardTitle>
              <CardDescription>
                Create Base URL nodes, attach each to a pool group, and set one
                active profile for live relay traffic.
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <EndpointProfileForm profile={editProfile} />
          <div className="mt-5 overflow-auto rounded-xl border border-white/5">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="sticky top-0 z-10 bg-[#1e1e1e]/95 text-xs uppercase tracking-wider text-neutral-500 backdrop-blur">
                <tr className="border-b border-primary/15">
                  <th className="px-3 py-3">Name</th>
                  <th className="px-3 py-3">Base URL</th>
                  <th className="px-3 py-3">Pool group</th>
                  <th className="px-3 py-3">Billing</th>
                  <th className="px-3 py-3">Status</th>
                  <th className="px-3 py-3">Actions</th>
                </tr>
              </thead>
              <tbody>
                {profiles.map((profile) => (
                  <MotionRow key={profile.name} className="border-b border-white/5 transition-colors hover:bg-white/[0.02]">
                    <td className="px-3 py-3 font-medium">{profile.name}</td>
                    <td className="px-3 py-3 text-neutral-400">
                      {profile.claude_base_url}
                    </td>
                    <td className="px-3 py-3">{profile.pool_group}</td>
                    <td className="px-3 py-3">
                      {profile.billing_mode === "token_based" ? (
                        <div className="flex flex-col text-xs text-neutral-400">
                          <span>{formatCents(profile.input_cost_per_m)}/1M in</span>
                          <span>{formatCents(profile.output_cost_per_m)}/1M out</span>
                        </div>
                      ) : (
                        <span className="text-xs text-neutral-400">{formatCents(profile.per_request_cost_cents)}/req</span>
                      )}
                    </td>
                    <td className="px-3 py-3">
                      <Badge variant={profile.active ? "success" : "outline"}>
                        {profile.active ? "Active" : "Inactive"}
                      </Badge>
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex flex-wrap gap-2">
                        <Button variant="secondary" size="sm" asChild>
                          <Link href={`?edit=${profile.name}`} scroll={false}>Edit</Link>
                        </Button>
                        <form
                          action={activateEndpointProfileAction.bind(
                            null,
                            profile.name,
                          )}
                        >
                          <Button type="submit" variant="secondary" size="sm">
                            Activate
                          </Button>
                        </form>
                        <form
                          action={deleteEndpointProfileAction.bind(
                            null,
                            profile.name,
                          )}
                        >
                          <ConfirmButton
                            type="submit"
                            variant="destructive"
                            size="sm"
                            message="Delete this Base URL and every key in its pool group?"
                          >
                            Delete
                          </ConfirmButton>
                        </form>
                      </div>
                    </td>
                  </MotionRow>
                ))}
                {profiles.length === 0 ? (
                  <tr>
                    <td
                      colSpan={5}
                      className="px-3 py-10 text-center text-neutral-500"
                    >
                      <div className="flex flex-col items-center gap-2">
                        <ServerOff className="size-6 opacity-40" />
                        <p>No Base URL profiles yet.</p>
                      </div>
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-2xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">Add a pooled key</CardTitle>
          <CardDescription>
            Add keys under the profile pool group tree.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AddPoolKeyForm poolGroups={poolGroups} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-2xl font-normal bg-clip-text text-transparent bg-gradient-to-r from-primary to-amber-200">Key hierarchy</CardTitle>
          <CardDescription>
            {pool.length} keys across {poolGroups.length} pool groups. Active
            group: {activePoolGroup}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-4">
            {poolGroups.map((group) => {
              const keys = keysByGroup.get(group) || [];
              const groupProfiles = profiles.filter(
                (profile) => profile.pool_group === group,
              );
              return (
                <div
                  key={group}
                  className="rounded-xl border border-white/5 p-4"
                >
                  <div className="mb-3 flex flex-wrap items-center gap-2">
                    <Badge
                      variant={
                        group === activePoolGroup ? "success" : "outline"
                      }
                    >
                      {group}
                    </Badge>
                    <span className="text-xs text-neutral-500">
                      Base URLs:{" "}
                      {groupProfiles.map((p) => p.name).join(", ") || "none"}
                    </span>
                  </div>
                  <div className="overflow-auto rounded-lg border border-white/5">
                    <table className="w-full min-w-[920px] text-left text-sm">
                      <thead className="bg-[#1e1e1e]/95 text-xs uppercase tracking-wider text-neutral-500">
                        <tr className="border-b border-primary/15">
                          <th className="px-3 py-3">Label</th>
                          <th className="px-3 py-3">Provider</th>
                          <th className="px-3 py-3">Status</th>
                          <th className="px-3 py-3">Balance</th>
                          <th className="px-3 py-3">Upstream Spend</th>
                          <th className="px-3 py-3">Added</th>
                          <th className="px-3 py-3">Top up</th>
                          <th className="px-3 py-3">Rotate</th>
                          <th className="px-3 py-3">Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {keys.map((k) => (
                          <MotionRow
                            key={k.id}
                            className="border-b border-white/5 align-top transition-colors hover:bg-white/[0.02]"
                          >
                            <td className="px-3 py-3">{k.label || "-"}</td>
                            <td className="px-3 py-3 uppercase text-neutral-400">
                              {k.provider}
                            </td>
                            <td className="px-3 py-3">
                              <Badge
                                variant={
                                  k.active
                                    ? "success"
                                    : k.exhausted_at
                                      ? "warning"
                                      : "destructive"
                                }
                              >
                                {k.exhausted_at
                                  ? "Exhausted"
                                  : k.active
                                    ? "Active"
                                    : "Disabled"}
                              </Badge>
                            </td>
                            <td className="px-3 py-3">
                              {formatCents(k.remaining_cents)} left of{" "}
                              {formatCents(k.balance_cents)}
                            </td>
                            <td className="px-3 py-3">
                              <UpstreamSpendButton id={k.id} getUsage={getUpstreamUsageAction} />
                            </td>
                            <td className="px-3 py-3 text-neutral-500">
                              {formatDateTime(k.created_at)}
                            </td>
                            <td className="px-3 py-3">
                              <TopUpPoolKeyForm id={k.id} />
                            </td>
                            <td className="px-3 py-3">
                              <RotatePoolKeyForm
                                id={k.id}
                                defaultLabel={k.label || ""}
                              />
                            </td>
                            <td className="px-3 py-3">
                              <div className="flex flex-wrap gap-2">
                                <form
                                  action={
                                    k.active
                                      ? disablePoolKeyAction.bind(null, k.id)
                                      : enablePoolKeyAction.bind(null, k.id)
                                  }
                                >
                                  <Button
                                    type="submit"
                                    variant="secondary"
                                    size="sm"
                                  >
                                    {k.active ? "Disable" : "Enable"}
                                  </Button>
                                </form>
                                <form
                                  action={deletePoolKeyAction.bind(null, k.id)}
                                >
                                  <ConfirmButton
                                    type="submit"
                                    variant="destructive"
                                    size="sm"
                                    message="Delete this key?"
                                  >
                                    Delete
                                  </ConfirmButton>
                                </form>
                              </div>
                            </td>
                          </MotionRow>
                        ))}
                        {keys.length === 0 ? (
                          <tr>
                            <td
                              colSpan={8}
                              className="px-3 py-10 text-center text-neutral-500"
                            >
                              <div className="flex flex-col items-center gap-2">
                                <KeyRound className="size-6 opacity-40" />
                                <p>No keys in this pool group.</p>
                              </div>
                            </td>
                          </tr>
                        ) : null}
                      </tbody>
                    </table>
                  </div>
                </div>
              );
            })}
            {poolGroups.length === 0 ? (
              <p className="rounded-xl border border-white/5 p-6 text-center text-neutral-500">
                No pooled keys yet. Add one above.
              </p>
            ) : null}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
