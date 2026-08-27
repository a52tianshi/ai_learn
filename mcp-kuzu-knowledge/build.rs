// This file has no logic of its own. Its only purpose is to make the
// `[build-dependencies] cxx-build = "=1.0.138"` pin in Cargo.toml take
// effect — Cargo only resolves build-dependencies for crates that declare
// a build script. That pin exists to match kuzu's own exact `cxx = "=1.0.138"`
// pin; without it, cxx-build can resolve to a newer version whose C++ codegen
// embeds a different symbol-name scheme than the `cxx` crate's Rust-side
// expects, causing "symbol(s) not found" linker errors specifically when
// real code calls into kuzu (e.g. `cargo test`, since `cargo build`'s unused
// `mod db;` gets dead-code-eliminated and never needs the symbols resolved).
fn main() {}
