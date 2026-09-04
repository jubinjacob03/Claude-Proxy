"use server";

import { redirect } from "next/navigation";
import { headers } from "next/headers";
import { createSession, verifyPassword } from "@/lib/auth";
import { rateLimit, clientIpFromHeaders } from "@/lib/rateLimit";

export async function loginAction(_prevState, formData) {
  const headerStore = await headers();
  const ip = clientIpFromHeaders(headerStore);
  const username = String(formData.get("username") || "").trim();
  const limited = rateLimit(`login:${ip}:${username.toLowerCase()}`, {
    limit: 10,
    windowMs: 60000,
  });
  if (!limited.allowed) {
    return { error: "Too many attempts. Try again in a minute." };
  }

  const password = String(formData.get("password") || "");
  if (!username || !password) {
    return { error: "Enter both a username and a password." };
  }
  if (!verifyPassword(username, password)) {
    return { error: "Incorrect username or password." };
  }

  await createSession(username);
  redirect("/dashboard");
}
