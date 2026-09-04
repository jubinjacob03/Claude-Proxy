import { spawn } from "node:child_process";
import { join } from "node:path";

const rootDir = join(process.cwd(), "..");
const exeName = process.platform === "win32" ? "claude-relay.exe" : "claude-relay";
const exePath = join(rootDir, exeName);

import { readFileSync, existsSync } from "node:fs";

// Load .env from root directory manually
const envPath = join(rootDir, ".env");
if (existsSync(envPath)) {
  const envContent = readFileSync(envPath, "utf-8");
  for (const line of envContent.split("\n")) {
    const trimmed = line.trim();
    if (trimmed && !trimmed.startsWith("#")) {
      const splitIdx = trimmed.indexOf("=");
      if (splitIdx !== -1) {
        const key = trimmed.slice(0, splitIdx).trim();
        let val = trimmed.slice(splitIdx + 1).trim();
        // Remove surrounding quotes if they exist
        if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
          val = val.slice(1, -1);
        }
        // Unescape escaped dollar signs
        val = val.replace(/\$\$/g, '$');
        process.env[key] = val;
      }
    }
  }
}

console.log("[dev-with-relay] building claude-relay...");

const build = spawn("go", ["build", "-o", exeName, "./cmd/claude-relay"], {
  cwd: rootDir,
  stdio: "inherit",
  shell: true,
});

build.on("close", (code) => {
  if (code !== 0) {
    console.error(`[dev-with-relay] go build failed with code ${code}`);
    process.exit(1);
  }

  console.log("[dev-with-relay] starting relay...");
  const relay = spawn(exePath, [], {
    cwd: rootDir,
    stdio: "inherit",
    shell: true,
  });

  console.log("[dev-with-relay] starting next.js...");
  const next = spawn("npx", ["next", "dev", "-p", "8090"], {
    cwd: process.cwd(),
    stdio: "inherit",
    shell: true,
  });

  const cleanup = () => {
    relay.kill();
    next.kill();
    process.exit();
  };

  process.on("SIGINT", cleanup);
  process.on("SIGTERM", cleanup);
  
  relay.on("close", (code) => {
    console.log(`[dev-with-relay] relay exited (${code}); stopping web.`);
    next.kill();
    process.exit(code || 0);
  });
  
  next.on("close", (code) => {
    console.log(`[dev-with-relay] next exited (${code}); stopping relay.`);
    relay.kill();
    process.exit(code || 0);
  });
});
