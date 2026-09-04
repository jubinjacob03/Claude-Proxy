import { createHmac, timingSafeEqual, scryptSync } from "node:crypto";
import { cache } from "react";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

const SESSION_COOKIE = "cp_admin_session";
const SESSION_TTL_MS = 1000 * 60 * 60 * 24 * 7;

function sessionKey() {
  if (process.env.SESSION_SECRET) return process.env.SESSION_SECRET;
  if (process.env.NODE_ENV !== "production") {
    return "change-this-development-session-secret";
  }
  throw new Error("SESSION_SECRET must be configured in production");
}

function cookieSecure() {
  return process.env.COOKIE_SECURE !== "false";
}

function signature(payload) {
  return createHmac("sha256", sessionKey()).update(payload).digest("base64url");
}

export async function createSession(username) {
  const payload = Buffer.from(
    JSON.stringify({ username, expiresAt: Date.now() + SESSION_TTL_MS }),
  ).toString("base64url");
  const cookieStore = await cookies();
  cookieStore.set(SESSION_COOKIE, `${payload}.${signature(payload)}`, {
    httpOnly: true,
    sameSite: "strict",
    secure: cookieSecure(),
    path: "/",
    maxAge: SESSION_TTL_MS / 1000,
  });
}

export async function clearSession() {
  const cookieStore = await cookies();
  cookieStore.delete(SESSION_COOKIE);
}

function readSession(value) {
  if (!value) return null;
  const separator = value.indexOf(".");
  if (separator < 1) return null;
  const payload = value.slice(0, separator);
  const receivedSignature = value.slice(separator + 1);
  const expectedSignature = signature(payload);
  if (
    receivedSignature.length !== expectedSignature.length ||
    !timingSafeEqual(
      Buffer.from(receivedSignature),
      Buffer.from(expectedSignature),
    )
  ) {
    return null;
  }
  try {
    const session = JSON.parse(
      Buffer.from(payload, "base64url").toString("utf8"),
    );
    if (session.expiresAt < Date.now()) return null;
    return session;
  } catch {
    return null;
  }
}

export const getCurrentAdmin = cache(async () => {
  const cookieStore = await cookies();
  const session = readSession(cookieStore.get(SESSION_COOKIE)?.value);
  return session ? { username: session.username } : null;
});

export async function requireAdmin() {
  const admin = await getCurrentAdmin();
  if (!admin) redirect("/login?error=Please+sign+in+to+continue");
  return admin;
}

export function verifyPassword(username, password) {
  const expectedUser = process.env.ADMIN_USER || "";
  const stored = process.env.ADMIN_PASSWORD_HASH || "";
  if (!expectedUser || !stored) return false;

  const userOk = timingSafeEqual(
    Buffer.from(username.toLowerCase().slice(0, 64).padEnd(64, "\0")),
    Buffer.from(expectedUser.toLowerCase().slice(0, 64).padEnd(64, "\0")),
  );

  const [scheme, saltHex, hashHex] = stored.split(":");
  if (scheme !== "scrypt" || !saltHex || !hashHex) return false;
  const salt = Buffer.from(saltHex, "hex");
  const expected = Buffer.from(hashHex, "hex");
  const got = scryptSync(password, salt, expected.length);
  const passOk =
    got.length === expected.length && timingSafeEqual(got, expected);

  return userOk && passOk;
}
