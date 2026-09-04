import { NextResponse } from "next/server";
import { clientIp, rateLimit } from "@/lib/rateLimit";

const protectedPrefixes = ["/dashboard", "/licenses", "/pool", "/usage"];

function isProtected(pathname) {
  return protectedPrefixes.some(
    (prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`),
  );
}

export function proxy(request) {
  const { pathname } = request.nextUrl;

  if (pathname === "/login" && request.method === "POST") {
    const ip = clientIp(request);
    const limited = rateLimit(`login:${ip}`, { limit: 10, windowMs: 60000 });
    if (!limited.allowed) {
      return NextResponse.json(
        { error: "Too many attempts. Try again in a minute." },
        {
          status: 429,
          headers: { "Retry-After": String(limited.retryAfter) },
        },
      );
    }
  }

  if (isProtected(pathname)) {
    const ip = clientIp(request);
    const limited = rateLimit(`admin:${ip}`, { limit: 300, windowMs: 60000 });
    if (!limited.allowed) {
      return NextResponse.json(
        { error: "Too many admin requests. Slow down and retry." },
        {
          status: 429,
          headers: { "Retry-After": String(limited.retryAfter) },
        },
      );
    }
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/login", "/dashboard/:path*", "/licenses/:path*", "/pool/:path*", "/usage/:path*"],
};
