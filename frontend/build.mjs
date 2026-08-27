// Deterministic frontend build: copies the single-page console sources into
// dist/ so the Go backend can serve them. No external dependencies are
// required, which keeps the build reproducible via `npm ci && npm run build`.
import { cp, mkdir, readdir } from "node:fs/promises";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(fileURLToPath(import.meta.url));
const src = join(root, "src");
const dist = join(root, "dist");

await mkdir(dist, { recursive: true });

if (!existsSync(src)) {
  console.error("missing frontend/src directory");
  process.exit(1);
}

const entries = await readdir(src);
for (const entry of entries) {
  await cp(join(src, entry), join(dist, entry));
}

console.log(`built ${entries.length} frontend file(s) into dist/`);
