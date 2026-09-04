import { relay } from "@/lib/relay";
import { requireAdmin } from "@/lib/auth";

function escapeCsv(value) {
  const text = String(value ?? "");
  if (/[",\n]/.test(text)) {
    return `"${text.replaceAll('"', '""')}"`;
  }
  return text;
}

export async function GET(request) {
  await requireAdmin();
  const { searchParams } = new URL(request.url);
  const q = searchParams.get("q") || "";
  const status = searchParams.get("status") || "";
  const { licenses } = await relay.listLicenses({ q, status });
  const rows = [
    [
      "id",
      "key_hint",
      "active",
      "bound",
      "hwid",
      "quota_cents",
      "spent_cents",
      "remaining_cents",
      "note",
      "created_at",
      "bound_at",
      "last_seen_at",
    ],
    ...licenses.map((license) => [
      license.id,
      license.key_hint,
      license.active,
      license.bound,
      license.hwid,
      license.quota_cents,
      license.spent_cents,
      license.remaining_cents,
      license.note,
      license.created_at,
      license.bound_at,
      license.last_seen_at,
    ]),
  ];
  const body = rows.map((row) => row.map(escapeCsv).join(",")).join("\n");
  return new Response(body, {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": 'attachment; filename="licenses.csv"',
    },
  });
}
