// Copied from KeyAuth verbatim. In-memory, per-process; fine at this scale
// since the relay - not this site - is the only thing that must survive a
// restart with its state intact.
const buckets = new Map();
let lastSweep = Date.now();

function resolveIp(get) {
  if (process.env.TRUST_PROXY !== "true") return "unknown";
  const real = get("x-real-ip");
  if (real) return real.trim();
  const forwarded = get("x-forwarded-for");
  if (forwarded) {
    const parts = forwarded
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean);
    if (parts.length) return parts[parts.length - 1];
  }
  return "unknown";
}

export function clientIp(request) {
  return resolveIp((key) => request.headers.get(key));
}

export function clientIpFromHeaders(headerStore) {
  return resolveIp((key) => headerStore.get(key));
}

export function rateLimit(key, { limit = 30, windowMs = 10000 } = {}) {
  const now = Date.now();
  if (now - lastSweep > 60000) {
    for (const [bucketKey, bucket] of buckets) {
      if (bucket.reset < now) buckets.delete(bucketKey);
    }
    lastSweep = now;
  }
  let bucket = buckets.get(key);
  if (!bucket || bucket.reset < now) {
    bucket = { count: 0, reset: now + windowMs };
    buckets.set(key, bucket);
  }
  bucket.count += 1;
  return {
    allowed: bucket.count <= limit,
    retryAfter: Math.max(1, Math.ceil((bucket.reset - now) / 1000)),
  };
}
