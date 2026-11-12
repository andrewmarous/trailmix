# Run the Mapgar map uploader

Use `npm run upload:map` for the configured map archive.
The uploader records confirmed multipart parts and byte offsets.
It retries failed ranges and parts, checks source identity, and omits whole-file MD5 verification.

## Evidence

The November 10, 2026 implementation passed 15 local tests, typechecking, and Worker bundling.
That check did not establish live R2 recovery.
The command uses its configured source. This note does not establish regional coverage.
