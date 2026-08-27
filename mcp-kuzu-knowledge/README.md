# mcp-kuzu-knowledge

Local Rust MCP server backed by an embedded Kùzu graph database. Exposes 5
tools (`search_concepts`, `get_neighbors`, `add_concept`, `add_relation`,
`update_proficiency`) over MCP Streamable HTTP, plus a read-only web page to
browse the graph.

## Prerequisites

- Rust toolchain (`rustup`)
- `cmake` and a C++ toolchain (macOS: `brew install cmake`, `xcode-select --install`)
  — the first build compiles Kùzu's C++ core from source and takes several minutes.
- Do not remove `build.rs` or the `[build-dependencies] cxx-build = "=1.0.138"` pin in
  Cargo.toml — they work around a real version-skew bug in the published `kuzu` crate
  (mismatched `cxx`/`cxx-build` versions producing incompatible FFI symbol names).

## Run

```bash
./run.sh
# or: PORT=8787 DATA_DIR=./data cargo run --release
```

- MCP endpoint: `http://127.0.0.1:8787/mcp`
- Web UI: `http://127.0.0.1:8787/`

## Configure Claude Code

Add an HTTP MCP server pointing at the running instance (check
`claude mcp add --help` for the exact flag names in your installed version):

```bash
claude mcp add --transport http kuzu-knowledge http://127.0.0.1:8787/mcp
```

## Configure Gemini CLI

Add an equivalent HTTP MCP server entry in your Gemini CLI config, pointing
at `http://127.0.0.1:8787/mcp`. Confirm your installed Gemini CLI version
supports HTTP-transport MCP servers before relying on this — it varies by
release.

## Known limitations

- No auth; binds to `127.0.0.1` only.
- No autostart (no launchd unit) — start manually when you need it.
- No batch import — the graph starts empty and grows via `add_concept` /
  `add_relation` calls made during normal CC/Gemini conversations.
