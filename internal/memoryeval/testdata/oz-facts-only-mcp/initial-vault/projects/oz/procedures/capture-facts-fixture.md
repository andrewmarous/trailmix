# Capture a reusable facts fixture

The recorded snapshot contains 30 published facts.
Determination failed, but the facts fixture remains valid.

## MCP limitation

The MCP query layer requires a completed determination linked to a fact set.
The facts-only archive does not meet that binding contract.
The launcher rejects the archive because completed-report bindings are missing.

## Evidence

The October 18, 2026 restore returned the facts, but MCP reads returned `saved_run_not_found`.
This describes the original implementation, not a new verification.
