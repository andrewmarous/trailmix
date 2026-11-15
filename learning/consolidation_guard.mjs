// Loaded explicitly only for consolidation; never installed in user settings.
import { realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

export default function (pi) {
  const root = realpathSync(process.cwd());
  let requests = 0;
  let bytes = 0;
  pi.on("session_start", () => {
    writeFileSync(process.env.TMIX_CONSOLIDATION_GUARD_STATUS, "ready", { mode: 0o600 });
    pi.setActiveTools(["read", "grep", "find", "ls"]);
  });
  const fail = (ctx, reason) => {
    writeFileSync(process.env.TMIX_CONSOLIDATION_GUARD_STATUS, reason, { mode: 0o600 });
    ctx.abort();
  };
  pi.on("before_provider_request", (_event, ctx) => {
    if (++requests > 24) fail(ctx, "consolidation exceeded 24 model turns");
  });
  pi.on("tool_call", (event) => {
    if (!["read", "grep", "find", "ls"].includes(event.toolName)) {
      return { block: true, reason: "Consolidation permits read-only inspection only", terminate: true };
    }
    try {
      const raw = event.input.path ?? ".";
      const path = realpathSync(resolve(root, raw.replace(/^@/, "")));
      const rel = relative(root, path);
      if (isAbsolute(rel) || rel === ".." || rel.startsWith(`..${sep}`)) {
        return { block: true, reason: "Read outside the consolidation bundle denied" };
      }
      event.input.path = path;
    } catch {
      return { block: true, reason: "Read path does not exist in the consolidation bundle" };
    }
  });
  pi.on("tool_result", (event, ctx) => {
    bytes += Buffer.byteLength(JSON.stringify(event.content), "utf8");
    if (bytes > Number(process.env.TMIX_CONSOLIDATION_READ_BYTES)) {
      fail(ctx, "consolidation exceeded its tool-read budget");
      return { content: [{ type: "text", text: "Consolidation tool-read budget exhausted" }], isError: true };
    }
  });
}
