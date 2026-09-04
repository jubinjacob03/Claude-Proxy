// Run with: npm run hash-password -- "your-password-here"
// Prints an ADMIN_PASSWORD_HASH value to put in .env. The plaintext password
// is never written anywhere by this script.
//
// Colon separated, not "$" separated: dotenv expands "$name" inside .env
// values, which would silently truncate the hash.
import { randomBytes, scryptSync } from "node:crypto";

const password = process.argv[2];
if (!password) {
  console.error('usage: npm run hash-password -- "<password>"');
  process.exit(1);
}

const salt = randomBytes(16);
const hash = scryptSync(password, salt, 64);
console.log(`scrypt:${salt.toString("hex")}:${hash.toString("hex")}`);
