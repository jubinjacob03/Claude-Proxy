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
  const licenseId = searchParams.get("license_id") || "";
  const q = searchParams.get("q") || "";
  const status = searchParams.get("status") || "";
  const { events } = await relay.usage({ license_id: licenseId, q, status });
  const rows = [
    [
      "id",
      "license_id",
      "pool_key_id",
      "provider",
      "model",
      "cost_cents",
      "streamed",
      "status_code",
      "created_at",
    ],
    ...events.map((event) => [
      event.id,
      event.license_id,
      event.pool_key_id,
      event.provider,
      event.model,
      event.cost_cents,
      event.streamed,
      event.status_code,
      event.created_at,
    ]),
  ];
  const body = rows.map((row) => row.map(escapeCsv).join(",")).join("\n");
  return new Response(body, {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": 'attachment; filename="usage.csv"',
    },
  });
}
