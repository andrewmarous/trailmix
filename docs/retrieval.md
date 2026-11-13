# Local retrieval (draft)

`tmix index` builds the local Markdown index. `tmix search` reads it without modification.
There is no transcript index, reranking, daemon, or automatic runtime installer.

## Configuration

Choose a retrieval mode in the shared TOML configuration:

```toml
[retrieval]
mode = "text"
# state_dir = "/absolute/path/to/local/index"
```

Text mode needs only SQLite/FTS5. Neither index builds nor searches require llama.cpp or model files.
For hybrid retrieval, configure explicit local artifacts:

```toml
[retrieval]
mode = "hybrid"
runtime = "/absolute/path/to/llama-server"
model = "/absolute/path/to/Qwen3-Embedding-0.6B-Q8_0.gguf"
```

Text is the default mode. Hybrid requires explicit configuration.
In hybrid mode, an empty model path produces an error, not a download or text fallback.
A runtime command name can use PATH. Model and state paths must be absolute paths, or start with `~/`.
Configuration changes do not build or modify indexes.

## Build an index

With the configuration complete, run `tmix index`.
Text mode indexes whole Markdown sections without token sizing or vector generation.
Hybrid mode builds both BM25 and vector indexes. Flags can override its configured artifact paths:

```sh
tmix --vault /path/to/vault index \
  --runtime /path/to/llama-server \
  --model /path/to/Qwen3-Embedding-0.6B-Q8_0.gguf
```

The command returns a JSON report with note, passage, generated-vector, and reused-vector counts.
The default database location is the machine cache directory, scoped to the absolute vault path.
Use `--state-dir PATH` to select another directory outside the vault.
Search uses the same `retrieval.state_dir` or default directory.
The database contains private note text and is not encrypted. It must not enter vault sync.
Text search can use a hybrid index without a runtime. Hybrid search requires an index with matching embeddings.
A text-only rebuild removes active embedding associations, but retains cached vectors for a later hybrid rebuild.

## Search

```sh
tmix search --limit 10 "prevent concurrent overwrites"
tmix search --json "baseline checks"
```

Text search quotes query terms and joins them with OR in FTS5, then orders passages by BM25.
Queries are plain text, not raw FTS syntax. Matching follows the FTS5 tokenizer, not substring matching.
Hybrid search embeds the query once with Qwen's retrieval instruction and computes exact cosine distance.
Each channel supplies up to 100 passages from the same committed index generation.
Equal-weight reciprocal rank fusion adds `1 / (60 + rank)` for each channel that retrieves a passage.
There is no reranker. Ties use path, source position, then passage ID.

Search validates live source hashes and byte ranges, then keeps the best valid passage per note.
The limit applies after note deduplication. The bounded candidate pool can produce fewer notes than the requested limit.
Text and JSON output retain `path`, `line`, and `snippet`. Snippets come from original Markdown.
Changed, deleted, unsafe, or invalid source pointers are excluded, with warnings on stderr.
If all candidates are excluded, stderr explicitly reports that no valid indexed matches remain.
Other read errors fail the search. Search never silently rebuilds or switches modes.
New or previously unmatched changed notes require an explicit index build before discovery.

## Runtime lifetime

The command starts one server when uncached inputs need token sizing or embeddings.
An unchanged corpus starts no server. Token sizing and embedding generation share the same process.
The server binds to loopback with an operation-specific API key.
It stays alive across batches and exits when the indexing operation finishes, fails, or receives normal cancellation.
There is no model download during indexing.

Text search starts no runtime. Hybrid search starts one runtime for one query embedding, then stops and reaps it.
SQLite computes vector distances after the runtime stops. No model remains resident for result fusion or source validation.
Search holds no database transaction during inference. Both candidate rankings share one read transaction afterward.
Search rechecks embedding identity after inference, in case an index replacement selected a different model.

Startup has a two-minute deadline. Requests have a two-minute deadline.
Normal shutdown allows five seconds before forced termination.
An uncatchable parent termination can leave a server process behind on platforms without parent-death handling.
This draft does not claim crash-proof child cleanup.

## Passage and index format

The `markdown-v2` format partitions notes at document-level Markdown headings, with no overlap.
Goldmark parses CommonMark and GFM structures, including tables and fenced code.
A parent section owns its introductory text; nested sections own their content.
Heading-only sections emit no passage, but their headings remain in descendant context.
A note without headings forms one section. Text before the first heading forms a preamble section.

Each passage receives the note title, path, and heading ancestry.
The exact same contextualized text feeds embedding generation and the FTS5 index.
Original Markdown remains separate from that context.

In hybrid builds, sections stay whole unless their complete embedding input exceeds 2,032 tokens, including special tokens.
The runtime tokenizer enforces this ceiling. Two passages share a 4,096-token batch, with framing headroom.
Oversized sections split between blocks first; oversized blocks fall back to line/sentence and UTF-8 boundaries.
A context prefix that leaves no room for source text fails explicitly rather than silently losing headings.
Splitting does not add overlap or copy source text into neighboring passages.

Each passage stores original byte offsets, line bounds, containing-section offsets, and the source note's content hash.
Search checks that hash before applying offsets to a live note.
Original Markdown evidence references remain in passage text. The index does not invent or resolve evidence references.

The runtime uses last-token pooling and L2 normalization. Each vector has 1,024 dimensions.
Document passages have no query instruction. Query embeddings include Qwen's retrieval instruction.

SQLite stores passages, model identity, and reusable vector blobs.
FTS5 supplies the lexical index. sqlite-vec supplies exact vector distance functions over the blobs.
This draft does not use a `vec0` virtual table or an ANN index.

The cache identity includes model bytes, executable bytes, pooling, normalization, dimensions, and the passage-format version.
Passage hashes cover contextualized text, not source offsets, so moved unchanged sections can reuse their vectors.
Changes to these inputs require new embeddings. Runtime library changes outside the executable are not fingerprinted.

Completed batches survive interruption. The last complete index remains active until replacement commits.
A second source scan rejects activation if notes changed during the build.
This scan is not a filesystem snapshot or a lock against later Obsidian edits.
Deleted notes leave the active index on the next successful build.
Schema version 2 adds source-range fields without deleting version 1 passages or vectors.
Old rows have no source hash until a successful rebuild; they are not valid versioned source pointers.
The new passage format requires rebuilding the active index, while failed builds preserve the previous active contents.
Unused vector-cache entries remain available for retries; automatic cache pruning is not implemented.

## Dependency compatibility

The draft uses the upstream sqlite-vec Go WASM bundle, without CGO.
Its WASM host ABI requires a compatible ncruces SQLite driver.
The tested pair is sqlite-vec bindings v0.1.6 and ncruces/go-sqlite3 v0.20.3.
A newer driver is not automatically compatible with this bundle.
Review dependency freshness and security before production release.

## Tested artifacts

The local verification used macOS arm64:

- Runtime: [llama.cpp b12327](https://github.com/ggml-org/llama.cpp/releases/tag/b12327).
- Asset: `llama-b12327-bin-macos-arm64.tar.gz`.
- Archive SHA-256: `4d36cc977af3239297c2a760f80093e3e3fbb9fd315fd342c8f15d6aed7a74ed`.
- Model: [Qwen3-Embedding-0.6B-GGUF](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/tree/370f27d7550e0def9b39c1f16d3fbaa13aa67728), `Qwen3-Embedding-0.6B-Q8_0.gguf`.
- Model SHA-256: `06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439`.

Runtime source and model weights are not vendored.
Other runtime builds, model artifacts, and hardware require separate verification.

## Verify

```sh
go test ./retrieval
TMIX_TEST_LLAMA=/path/to/llama-server \
TMIX_TEST_MODEL=/path/to/Qwen3-Embedding-0.6B-Q8_0.gguf \
  go test ./retrieval -run TestLiveQwenEmbeddings -v -count=1
```

Ordinary tests use synthetic notes and deterministic vectors. They do not download models or make paid requests.
The opt-in test starts real local inference and checks vector output and process cleanup.
