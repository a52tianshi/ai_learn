# Kùzu Knowledge Graph MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone Rust binary that embeds a Kùzu knowledge-graph
database, exposes 5 CRUD/query tools over MCP (Streamable HTTP transport) for
Claude Code / Gemini CLI, and serves a minimal read-only web frontend on the
same port for human browsing.

**Architecture:** Single Rust binary (`axum` HTTP server) with one embedded
Kùzu `Connection` shared behind a `tokio::sync::Mutex` so all reads/writes —
whether triggered by an MCP tool call or a REST GET — are serialized against
the single-writer embedded database. `rmcp`'s `StreamableHttpService` is
nested at `/mcp`; two JSON REST routes (`/api/concepts`, `/api/neighbors/{id}`)
and a static HTML page (`/`) live on the same `axum::Router`.

**Tech Stack:** Rust (edition 2021), `kuzu` 0.11.x (embedded graph DB,
compiles its C++ core from source), `rmcp` 3.1.4 (official MCP Rust SDK,
Streamable HTTP transport), `axum` 0.8, `tokio`, `serde`/`serde_json`,
`schemars`, `uuid`, `time`. Frontend: single static HTML file, vanilla JS +
vis-network via CDN.

**Spec:** `docs/superpowers/specs/2026-08-27-kuzu-knowledge-mcp-design.md`

**Project location:** This code does NOT live in the `ai_learn` repo. Create
and work in a new standalone directory: `~/mcp-kuzu-knowledge/` (a fresh git
repo). All file paths below are relative to that directory, not to
`ai_learn`.

## Global Constraints

- Server binds only to `127.0.0.1` — never `0.0.0.0`. No auth (spec accepts
  this for a single-machine tool).
- `proficiency` / `new_level` values must be validated to the inclusive range
  0–5 before touching the database.
- No batch-import script; the graph starts empty (spec: "从零开始").
- No launchd/autostart — manual `cargo run` / `run.sh` only.
- The database is opened by exactly one process, one `Connection`, guarded by
  one `tokio::sync::Mutex` — do not add a second connection or bypass the
  mutex, even for "read-only" paths, since Kùzu embeds a single-writer model.
- The `kuzu` crate compiles a C++ core from source on first build via `cmake`
  + `cxx-build`. This requires `cmake` and a working C++ toolchain installed
  on the machine (macOS: Xcode Command Line Tools, `brew install cmake`), and
  the first `cargo build` will take several minutes. This is expected, not a
  bug.
- The Rust code in this plan was written against the real, verified public
  APIs of `rmcp` 3.1.4 and `kuzu` 0.11.x (fetched from their upstream
  repositories during planning), but has not itself been compiled. After
  every task that adds Rust code, run `cargo build`/`cargo test` and treat
  the compiler as authoritative — fix any small signature mismatches against
  the real installed crate (check with `cargo doc --open -p kuzu` /
  `-p rmcp` or `~/.cargo/registry/src/**/kuzu-*/src` if something doesn't
  match) rather than guessing further.

---

## File Structure

```
~/mcp-kuzu-knowledge/
├── Cargo.toml
├── .gitignore
├── run.sh
├── README.md
├── src/
│   ├── main.rs      # process entrypoint, builds the axum Router, binds + serves
│   ├── db.rs         # Db struct: Kùzu connection, schema init, all query logic
│   ├── tools.rs       # MCP tool definitions (rmcp), wraps Db
│   └── web.rs         # REST handlers (/api/concepts, /api/neighbors/{id}) + static page
├── static/
│   └── index.html    # read-only frontend (search + vis-network graph)
└── data/              # Kùzu database files, created at runtime (gitignored)
```

---

## Task 1: Project scaffold and a building binary

**Files:**
- Create: `Cargo.toml`
- Create: `.gitignore`
- Create: `src/main.rs` (placeholder entrypoint, replaced fully in Task 5)
- Create: `run.sh`

**Interfaces:**
- Consumes: nothing (first task)
- Produces: a `cargo build`-able crate named `mcp-kuzu-knowledge` with `kuzu`,
  `rmcp`, `axum`, `tokio`, `serde`, `serde_json`, `schemars`, `uuid`, `time`,
  `anyhow` as dependencies, confirming the slow native `kuzu` build succeeds
  on this machine before any real logic is written.

- [ ] **Step 1: Create the project directory and initialize git**

```bash
mkdir -p ~/mcp-kuzu-knowledge
cd ~/mcp-kuzu-knowledge
git init
```

- [ ] **Step 2: Write `Cargo.toml`**

```toml
[package]
name = "mcp-kuzu-knowledge"
version = "0.1.0"
edition = "2021"
publish = false

[dependencies]
rmcp = { version = "3.1.4", features = ["server", "macros", "transport-streamable-http-server"] }
axum = { version = "0.8", features = ["macros"] }
tokio = { version = "1", features = ["full"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
schemars = "1.0"
kuzu = "0.11"
uuid = { version = "1", features = ["v4"] }
time = "0.3"
anyhow = "1"

[dev-dependencies]
tempfile = "3"
```

- [ ] **Step 3: Write `.gitignore`**

```
/target
/data
```

- [ ] **Step 4: Write a placeholder `src/main.rs`**

```rust
fn main() {
    println!("scaffold ok");
}
```

- [ ] **Step 5: Build and confirm it compiles (this will be slow the first time — kuzu compiles its C++ core from source)**

Run: `cargo build`
Expected: `Compiling mcp-kuzu-knowledge v0.1.0 ...` then `Finished` with no
errors. If `cmake` or a C++ compiler is missing, install them first
(`brew install cmake`, `xcode-select --install`) and re-run.

- [ ] **Step 6: Write `run.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export PORT="${PORT:-8787}"
export DATA_DIR="${DATA_DIR:-./data}"
cargo run --release
```

```bash
chmod +x run.sh
```

- [ ] **Step 7: Commit**

```bash
git add Cargo.toml .gitignore src/main.rs run.sh
git commit -m "scaffold: project builds with kuzu + rmcp + axum deps"
```

---

## Task 2: `db.rs` — connection, schema, `add_concept`, `search_concepts`

**Files:**
- Create: `src/db.rs`
- Modify: `src/main.rs` (declare `mod db;`)

**Interfaces:**
- Consumes: nothing beyond Task 1's crate deps
- Produces (used by Tasks 3, 4, 5, 6):
  - `pub struct Concept { pub id: String, pub label: String, pub category: String, pub proficiency: i64, pub details: String }` (`Clone`, `Debug`, `Serialize`)
  - `pub enum DbError { NotFound(String), InvalidInput(String), Query(String) }` (`Debug`)
  - `pub struct Db` with `pub fn open(path: &std::path::Path) -> Result<Self, DbError>`
  - `pub async fn Db::search_concepts(&self, keyword: &str) -> Result<Vec<Concept>, DbError>`
  - `pub async fn Db::add_concept(&self, label: &str, category: &str, proficiency: i64, details: &str) -> Result<Concept, DbError>`

- [ ] **Step 1: Write `src/db.rs` with the core connection, schema init, error type, and two functions**

```rust
use kuzu::{Connection, Database, LogicalType, SystemConfig, Value};
use serde::Serialize;
use std::collections::HashSet;
use std::path::Path;
use time::OffsetDateTime;
use tokio::sync::Mutex;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize)]
pub struct Concept {
    pub id: String,
    pub label: String,
    pub category: String,
    pub proficiency: i64,
    pub details: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct Edge {
    pub from: String,
    pub to: String,
    pub relation_type: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct Subgraph {
    pub nodes: Vec<Concept>,
    pub edges: Vec<Edge>,
}

#[derive(Debug)]
pub enum DbError {
    NotFound(String),
    InvalidInput(String),
    Query(String),
}

impl std::fmt::Display for DbError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            DbError::NotFound(m) => write!(f, "not found: {m}"),
            DbError::InvalidInput(m) => write!(f, "invalid input: {m}"),
            DbError::Query(m) => write!(f, "query error: {m}"),
        }
    }
}

impl std::error::Error for DbError {}

impl From<kuzu::Error> for DbError {
    fn from(e: kuzu::Error) -> Self {
        DbError::Query(e.to_string())
    }
}

fn as_string(v: &Value) -> Result<String, DbError> {
    match v {
        Value::String(s) => Ok(s.clone()),
        other => Err(DbError::Query(format!("expected STRING, got {other:?}"))),
    }
}

fn as_i64(v: &Value) -> Result<i64, DbError> {
    match v {
        Value::Int64(n) => Ok(*n),
        other => Err(DbError::Query(format!("expected INT64, got {other:?}"))),
    }
}

fn row_to_concept(row: &[Value]) -> Result<Concept, DbError> {
    Ok(Concept {
        id: as_string(&row[0])?,
        label: as_string(&row[1])?,
        category: as_string(&row[2])?,
        proficiency: as_i64(&row[3])?,
        details: as_string(&row[4])?,
    })
}

/// Runs `ddl` and treats an "already exists" failure as success, since this
/// crate's Kùzu version's support for `IF NOT EXISTS` on `CREATE ... TABLE`
/// wasn't verified during planning.
fn create_table_if_missing(conn: &Connection, ddl: &str) -> Result<(), DbError> {
    match conn.query(ddl) {
        Ok(_) => Ok(()),
        Err(e) if e.to_string().to_lowercase().contains("already exists") => Ok(()),
        Err(e) => Err(e.into()),
    }
}

pub struct Db {
    conn: Mutex<Connection<'static>>,
}

impl Db {
    pub fn open(path: &Path) -> Result<Self, DbError> {
        // Connection<'a> borrows Database<'a>; since this Db lives for the
        // whole process, leaking the Database to get a 'static reference is
        // the simplest way to store both together without unsafe code.
        let database: &'static Database = Box::leak(Box::new(Database::new(
            path,
            SystemConfig::default(),
        )?));
        let conn = Connection::new(database)?;

        create_table_if_missing(
            &conn,
            "CREATE NODE TABLE Concept(id STRING PRIMARY KEY, label STRING, category STRING, proficiency INT64, details STRING, created_at TIMESTAMP);",
        )?;
        create_table_if_missing(
            &conn,
            "CREATE REL TABLE RELATED_TO(FROM Concept TO Concept, relation_type STRING);",
        )?;

        Ok(Db {
            conn: Mutex::new(conn),
        })
    }

    pub async fn search_concepts(&self, keyword: &str) -> Result<Vec<Concept>, DbError> {
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(
            "MATCH (c:Concept) WHERE lower(c.label) CONTAINS lower($kw) OR lower(c.details) CONTAINS lower($kw) RETURN c.id, c.label, c.category, c.proficiency, c.details;",
        )?;
        let result = conn.execute(&mut stmt, vec![("kw", Value::String(keyword.to_string()))])?;
        let mut out = Vec::new();
        for row in result {
            out.push(row_to_concept(&row)?);
        }
        Ok(out)
    }

    pub async fn add_concept(
        &self,
        label: &str,
        category: &str,
        proficiency: i64,
        details: &str,
    ) -> Result<Concept, DbError> {
        if !(0..=5).contains(&proficiency) {
            return Err(DbError::InvalidInput(
                "proficiency must be between 0 and 5".to_string(),
            ));
        }
        let id = Uuid::new_v4().to_string();
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(
            "CREATE (c:Concept {id: $id, label: $label, category: $category, proficiency: $proficiency, details: $details, created_at: $created_at});",
        )?;
        conn.execute(
            &mut stmt,
            vec![
                ("id", Value::String(id.clone())),
                ("label", Value::String(label.to_string())),
                ("category", Value::String(category.to_string())),
                ("proficiency", Value::Int64(proficiency)),
                ("details", Value::String(details.to_string())),
                ("created_at", Value::Timestamp(OffsetDateTime::now_utc())),
            ],
        )?;
        Ok(Concept {
            id,
            label: label.to_string(),
            category: category.to_string(),
            proficiency,
            details: details.to_string(),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn open_test_db() -> (tempfile::TempDir, Db) {
        let dir = tempfile::tempdir().unwrap();
        let db = Db::open(&dir.path().join("testdb")).unwrap();
        (dir, db)
    }

    #[tokio::test]
    async fn add_then_search_finds_by_label() {
        let (_dir, db) = open_test_db();
        let created = db
            .add_concept("Rust Ownership", "language", 3, "borrow checker basics")
            .await
            .unwrap();

        let found = db.search_concepts("ownership").await.unwrap();
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].id, created.id);
        assert_eq!(found[0].proficiency, 3);
    }

    #[tokio::test]
    async fn search_matches_details_case_insensitively() {
        let (_dir, db) = open_test_db();
        db.add_concept("Kùzu", "database", 1, "Embedded GRAPH database")
            .await
            .unwrap();

        let found = db.search_concepts("graph").await.unwrap();
        assert_eq!(found.len(), 1);
    }

    #[tokio::test]
    async fn add_concept_rejects_out_of_range_proficiency() {
        let (_dir, db) = open_test_db();
        let err = db.add_concept("X", "cat", 9, "details").await.unwrap_err();
        assert!(matches!(err, DbError::InvalidInput(_)));
    }
}
```

- [ ] **Step 2: Declare the module in `src/main.rs`**

```rust
mod db;

fn main() {
    println!("scaffold ok");
}
```

- [ ] **Step 3: Run the tests**

Run: `cargo test`
Expected: 3 tests pass (`add_then_search_finds_by_label`,
`search_matches_details_case_insensitively`,
`add_concept_rejects_out_of_range_proficiency`). If a method name or type
doesn't match the installed `kuzu`/`tokio` versions, fix against the actual
compiler error before continuing.

- [ ] **Step 4: Commit**

```bash
git add src/db.rs src/main.rs
git commit -m "feat(db): schema init, add_concept, search_concepts"
```

---

## Task 3: `db.rs` — `add_relation`, `update_proficiency`

**Files:**
- Modify: `src/db.rs`

**Interfaces:**
- Consumes: `Db`, `Concept`, `DbError`, `row_to_concept` from Task 2
- Produces (used by Tasks 5, 6):
  - `pub async fn Db::add_relation(&self, from_id: &str, to_id: &str, relation_type: &str) -> Result<(), DbError>`
  - `pub async fn Db::update_proficiency(&self, concept_id: &str, new_level: i64) -> Result<Concept, DbError>`

- [ ] **Step 1: Add the two methods to `impl Db` in `src/db.rs`**

```rust
    pub async fn add_relation(
        &self,
        from_id: &str,
        to_id: &str,
        relation_type: &str,
    ) -> Result<(), DbError> {
        let conn = self.conn.lock().await;

        let mut exists_stmt = conn.prepare("MATCH (c:Concept {id: $id}) RETURN c.id;")?;

        let mut from_result =
            conn.execute(&mut exists_stmt, vec![("id", Value::String(from_id.to_string()))])?;
        if from_result.next().is_none() {
            return Err(DbError::NotFound(format!("concept not found: {from_id}")));
        }

        let mut to_result =
            conn.execute(&mut exists_stmt, vec![("id", Value::String(to_id.to_string()))])?;
        if to_result.next().is_none() {
            return Err(DbError::NotFound(format!("concept not found: {to_id}")));
        }

        let mut create_stmt = conn.prepare(
            "MATCH (a:Concept {id: $from_id}), (b:Concept {id: $to_id}) CREATE (a)-[:RELATED_TO {relation_type: $rt}]->(b);",
        )?;
        conn.execute(
            &mut create_stmt,
            vec![
                ("from_id", Value::String(from_id.to_string())),
                ("to_id", Value::String(to_id.to_string())),
                ("rt", Value::String(relation_type.to_string())),
            ],
        )?;
        Ok(())
    }

    pub async fn update_proficiency(
        &self,
        concept_id: &str,
        new_level: i64,
    ) -> Result<Concept, DbError> {
        if !(0..=5).contains(&new_level) {
            return Err(DbError::InvalidInput(
                "new_level must be between 0 and 5".to_string(),
            ));
        }
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(
            "MATCH (c:Concept {id: $id}) SET c.proficiency = $level RETURN c.id, c.label, c.category, c.proficiency, c.details;",
        )?;
        let mut result = conn.execute(
            &mut stmt,
            vec![
                ("id", Value::String(concept_id.to_string())),
                ("level", Value::Int64(new_level)),
            ],
        )?;
        match result.next() {
            Some(row) => row_to_concept(&row),
            None => Err(DbError::NotFound(format!(
                "concept not found: {concept_id}"
            ))),
        }
    }
```

- [ ] **Step 2: Add tests to the `tests` module in `src/db.rs`**

```rust
    #[tokio::test]
    async fn add_relation_links_two_existing_concepts() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "").await.unwrap();

        db.add_relation(&a.id, &b.id, "depends_on").await.unwrap();
    }

    #[tokio::test]
    async fn add_relation_errors_on_missing_concept() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();

        let err = db
            .add_relation(&a.id, "does-not-exist", "depends_on")
            .await
            .unwrap_err();
        assert!(matches!(err, DbError::NotFound(_)));
    }

    #[tokio::test]
    async fn update_proficiency_changes_value() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();

        let updated = db.update_proficiency(&a.id, 4).await.unwrap();
        assert_eq!(updated.proficiency, 4);
    }

    #[tokio::test]
    async fn update_proficiency_errors_on_missing_concept() {
        let (_dir, db) = open_test_db();
        let err = db
            .update_proficiency("does-not-exist", 4)
            .await
            .unwrap_err();
        assert!(matches!(err, DbError::NotFound(_)));
    }

    #[tokio::test]
    async fn update_proficiency_rejects_out_of_range() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();
        let err = db.update_proficiency(&a.id, 9).await.unwrap_err();
        assert!(matches!(err, DbError::InvalidInput(_)));
    }
```

- [ ] **Step 3: Run the tests**

Run: `cargo test`
Expected: all 8 tests (3 from Task 2 + 5 new) pass.

- [ ] **Step 4: Commit**

```bash
git add src/db.rs
git commit -m "feat(db): add_relation, update_proficiency"
```

---

## Task 4: `db.rs` — `get_neighbors`

**Files:**
- Modify: `src/db.rs`

**Interfaces:**
- Consumes: `Db`, `Concept`, `Edge`, `Subgraph`, `DbError`, `row_to_concept`,
  `as_string` from Tasks 2–3
- Produces (used by Tasks 5, 6):
  - `pub async fn Db::get_neighbors(&self, concept_id: &str, depth: i64) -> Result<Subgraph, DbError>`
  - Behavior: `depth` is clamped to `1..=5`. Returns `NotFound` if
    `concept_id` doesn't exist. `Subgraph.nodes` always includes the center
    concept itself plus every concept reachable within `depth` undirected
    hops (deduplicated). `Subgraph.edges` contains every `RELATED_TO` edge
    where both endpoints are in `nodes`.

- [ ] **Step 1: Add `get_neighbors` to `impl Db` in `src/db.rs`**

```rust
    pub async fn get_neighbors(&self, concept_id: &str, depth: i64) -> Result<Subgraph, DbError> {
        let depth = depth.clamp(1, 5);
        let conn = self.conn.lock().await;

        let mut center_stmt = conn.prepare(
            "MATCH (c:Concept {id: $id}) RETURN c.id, c.label, c.category, c.proficiency, c.details;",
        )?;
        let mut center_result = conn.execute(
            &mut center_stmt,
            vec![("id", Value::String(concept_id.to_string()))],
        )?;
        let center = match center_result.next() {
            Some(row) => row_to_concept(&row)?,
            None => {
                return Err(DbError::NotFound(format!(
                    "concept not found: {concept_id}"
                )))
            }
        };

        let neighbor_query = format!(
            "MATCH (c:Concept {{id: $id}})-[:RELATED_TO*1..{depth}]-(n:Concept) RETURN DISTINCT n.id, n.label, n.category, n.proficiency, n.details;"
        );
        let mut neighbor_stmt = conn.prepare(&neighbor_query)?;
        let neighbor_result = conn.execute(
            &mut neighbor_stmt,
            vec![("id", Value::String(concept_id.to_string()))],
        )?;

        let mut seen_ids: HashSet<String> = HashSet::new();
        seen_ids.insert(center.id.clone());
        let mut nodes = vec![center];
        for row in neighbor_result {
            let concept = row_to_concept(&row)?;
            if seen_ids.insert(concept.id.clone()) {
                nodes.push(concept);
            }
        }

        let ids: Vec<Value> = nodes.iter().map(|c| Value::String(c.id.clone())).collect();
        let mut edge_stmt = conn.prepare(
            "MATCH (a:Concept)-[r:RELATED_TO]->(b:Concept) WHERE a.id IN $ids AND b.id IN $ids RETURN a.id, b.id, r.relation_type;",
        )?;
        let edge_result = conn.execute(
            &mut edge_stmt,
            vec![("ids", Value::List(LogicalType::String, ids))],
        )?;
        let mut edges = Vec::new();
        for row in edge_result {
            edges.push(Edge {
                from: as_string(&row[0])?,
                to: as_string(&row[1])?,
                relation_type: as_string(&row[2])?,
            });
        }

        Ok(Subgraph { nodes, edges })
    }
```

- [ ] **Step 2: Add tests to the `tests` module**

```rust
    #[tokio::test]
    async fn get_neighbors_includes_center_and_direct_edge() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "").await.unwrap();
        db.add_relation(&a.id, &b.id, "depends_on").await.unwrap();

        let sub = db.get_neighbors(&a.id, 2).await.unwrap();
        let node_ids: HashSet<_> = sub.nodes.iter().map(|c| c.id.clone()).collect();
        assert_eq!(node_ids, HashSet::from([a.id.clone(), b.id.clone()]));
        assert_eq!(sub.edges.len(), 1);
        assert_eq!(sub.edges[0].relation_type, "depends_on");
    }

    #[tokio::test]
    async fn get_neighbors_respects_depth_limit() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "").await.unwrap();
        let c = db.add_concept("C", "cat", 0, "").await.unwrap();
        db.add_relation(&a.id, &b.id, "depends_on").await.unwrap();
        db.add_relation(&b.id, &c.id, "depends_on").await.unwrap();

        let sub = db.get_neighbors(&a.id, 1).await.unwrap();
        let node_ids: HashSet<_> = sub.nodes.iter().map(|n| n.id.clone()).collect();
        assert_eq!(node_ids, HashSet::from([a.id.clone(), b.id.clone()]));
        assert!(!node_ids.contains(&c.id));
    }

    #[tokio::test]
    async fn get_neighbors_errors_on_missing_concept() {
        let (_dir, db) = open_test_db();
        let err = db.get_neighbors("does-not-exist", 2).await.unwrap_err();
        assert!(matches!(err, DbError::NotFound(_)));
    }
```

- [ ] **Step 3: Run the tests**

Run: `cargo test`
Expected: all 11 tests pass.

- [ ] **Step 4: Commit**

```bash
git add src/db.rs
git commit -m "feat(db): get_neighbors"
```

---

## Task 5: `tools.rs` — MCP tools + Streamable HTTP wiring

**Files:**
- Create: `src/tools.rs`
- Modify: `src/main.rs` (real entrypoint: opens `Db`, builds the `axum`
  router with `/mcp` nested, binds and serves)

**Interfaces:**
- Consumes: `Db`, `Concept`, `Subgraph`, `DbError` from Tasks 2–4
- Produces (used by Task 6):
  - `pub struct KnowledgeGraphServer` implementing `rmcp::ServerHandler`,
    with `pub fn new(db: std::sync::Arc<Db>) -> Self`
  - `src/main.rs` builds an `axum::Router` with `/mcp` already nested; Task 6
    extends this same router with `/`, `/api/concepts`, `/api/neighbors/{id}`
    and a shared `.with_state(Arc<Db>)`

- [ ] **Step 1: Write `src/tools.rs`**

```rust
use std::sync::Arc;

use rmcp::{
    ErrorData as McpError, ServerHandler,
    handler::server::{router::tool::ToolRouter, wrapper::Parameters},
    model::{
        CallToolResult, ContentBlock, Implementation, ProtocolVersion, ServerCapabilities,
        ServerInfo,
    },
    schemars, tool, tool_handler, tool_router,
};
use serde::Deserialize;

use crate::db::{Db, DbError};

fn db_err_to_mcp(e: DbError) -> McpError {
    match e {
        DbError::NotFound(msg) => McpError::resource_not_found(msg, None),
        DbError::InvalidInput(msg) => McpError::invalid_params(msg, None),
        DbError::Query(msg) => McpError::internal_error(msg, None),
    }
}

fn json_result<T: serde::Serialize>(value: &T) -> Result<CallToolResult, McpError> {
    let json = serde_json::to_string(value).map_err(|e| McpError::internal_error(e.to_string(), None))?;
    Ok(CallToolResult::success(vec![ContentBlock::text(json)]))
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct SearchConceptsRequest {
    pub keyword: String,
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct GetNeighborsRequest {
    pub concept_id: String,
    #[serde(default = "default_depth")]
    pub depth: i64,
}
fn default_depth() -> i64 {
    2
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct AddConceptRequest {
    pub label: String,
    pub category: String,
    pub proficiency: i64,
    pub details: String,
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct AddRelationRequest {
    pub from_id: String,
    pub to_id: String,
    pub relation_type: String,
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct UpdateProficiencyRequest {
    pub concept_id: String,
    pub new_level: i64,
}

#[derive(Clone)]
pub struct KnowledgeGraphServer {
    db: Arc<Db>,
    tool_router: ToolRouter<KnowledgeGraphServer>,
}

#[tool_router]
impl KnowledgeGraphServer {
    pub fn new(db: Arc<Db>) -> Self {
        Self {
            db,
            tool_router: Self::tool_router(),
        }
    }

    #[tool(description = "Fuzzy-search knowledge graph concepts by keyword in their label or details")]
    async fn search_concepts(
        &self,
        Parameters(req): Parameters<SearchConceptsRequest>,
    ) -> Result<CallToolResult, McpError> {
        let results = self
            .db
            .search_concepts(&req.keyword)
            .await
            .map_err(db_err_to_mcp)?;
        json_result(&results)
    }

    #[tool(description = "Get the subgraph (nodes + edges) within N hops of a given concept id")]
    async fn get_neighbors(
        &self,
        Parameters(req): Parameters<GetNeighborsRequest>,
    ) -> Result<CallToolResult, McpError> {
        let subgraph = self
            .db
            .get_neighbors(&req.concept_id, req.depth)
            .await
            .map_err(db_err_to_mcp)?;
        json_result(&subgraph)
    }

    #[tool(description = "Add a new concept node to the personal knowledge graph")]
    async fn add_concept(
        &self,
        Parameters(req): Parameters<AddConceptRequest>,
    ) -> Result<CallToolResult, McpError> {
        let concept = self
            .db
            .add_concept(&req.label, &req.category, req.proficiency, &req.details)
            .await
            .map_err(db_err_to_mcp)?;
        json_result(&concept)
    }

    #[tool(description = "Add a RELATED_TO relation between two existing concepts")]
    async fn add_relation(
        &self,
        Parameters(req): Parameters<AddRelationRequest>,
    ) -> Result<CallToolResult, McpError> {
        self.db
            .add_relation(&req.from_id, &req.to_id, &req.relation_type)
            .await
            .map_err(db_err_to_mcp)?;
        Ok(CallToolResult::success(vec![ContentBlock::text("ok")]))
    }

    #[tool(description = "Update the proficiency level (0-5) of an existing concept")]
    async fn update_proficiency(
        &self,
        Parameters(req): Parameters<UpdateProficiencyRequest>,
    ) -> Result<CallToolResult, McpError> {
        let concept = self
            .db
            .update_proficiency(&req.concept_id, req.new_level)
            .await
            .map_err(db_err_to_mcp)?;
        json_result(&concept)
    }
}

#[tool_handler]
impl ServerHandler for KnowledgeGraphServer {
    fn get_info(&self) -> ServerInfo {
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_server_info(Implementation::from_build_env())
            .with_protocol_version(ProtocolVersion::V_2024_11_05)
            .with_instructions(
                "Personal knowledge graph over a local Kùzu database. Tools: search_concepts, get_neighbors, add_concept, add_relation, update_proficiency.".to_string(),
            )
    }
}
```

- [ ] **Step 2: Replace `src/main.rs` with the real entrypoint**

```rust
mod db;
mod tools;

use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use axum::Router;
use rmcp::transport::streamable_http_server::{
    StreamableHttpServerConfig, StreamableHttpService, session::local::LocalSessionManager,
};

use db::Db;
use tools::KnowledgeGraphServer;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let data_dir = std::env::var("DATA_DIR").unwrap_or_else(|_| "./data".to_string());
    let port: u16 = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8787);

    let db = Arc::new(Db::open(&PathBuf::from(data_dir))?);

    let mcp_service = {
        let db = db.clone();
        StreamableHttpService::new(
            move || Ok(KnowledgeGraphServer::new(db.clone())),
            LocalSessionManager::default().into(),
            StreamableHttpServerConfig::default(),
        )
    };

    let router: Router = Router::new().nest_service("/mcp", mcp_service);

    let addr: SocketAddr = ([127, 0, 0, 1], port).into();
    let listener = tokio::net::TcpListener::bind(addr).await?;
    println!("listening on http://{addr}");
    axum::serve(listener, router).await?;
    Ok(())
}
```

- [ ] **Step 3: Build and run**

Run: `cargo build`
Expected: builds with no errors (fix any real signature mismatches against
the installed `rmcp`/`axum` crate now, per Global Constraints).

Run: `cargo run` in one terminal, then in another:
```bash
curl -s http://127.0.0.1:8787/mcp -X POST \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```
Expected: a JSON-RPC response containing `"serverInfo"` (confirms the MCP
endpoint is live). Stop the server (Ctrl-C) before continuing.

- [ ] **Step 4: Commit**

```bash
git add src/tools.rs src/main.rs
git commit -m "feat(mcp): expose 5 knowledge-graph tools over streamable HTTP"
```

---

## Task 6: `web.rs` — REST API + static frontend

**Files:**
- Create: `src/web.rs`
- Create: `static/index.html`
- Modify: `src/main.rs` (mount `/`, `/api/concepts`, `/api/neighbors/{id}`,
  add `.with_state(db)`)

**Interfaces:**
- Consumes: `Db`, `DbError`, `Concept`, `Subgraph` from Tasks 2–4
- Produces: nothing further consumed by later tasks (final integration task)

- [ ] **Step 1: Write `src/web.rs`**

```rust
use std::sync::Arc;

use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{Html, IntoResponse, Response},
};
use serde::Deserialize;

use crate::db::{Db, DbError};

const INDEX_HTML: &str = include_str!("../static/index.html");

pub async fn index() -> Html<&'static str> {
    Html(INDEX_HTML)
}

#[derive(Deserialize)]
pub struct SearchQuery {
    q: Option<String>,
}

pub async fn list_concepts(
    State(db): State<Arc<Db>>,
    Query(params): Query<SearchQuery>,
) -> Response {
    let keyword = params.q.unwrap_or_default();
    match db.search_concepts(&keyword).await {
        Ok(concepts) => Json(concepts).into_response(),
        Err(e) => api_error(e),
    }
}

#[derive(Deserialize)]
pub struct NeighborsQuery {
    depth: Option<i64>,
}

pub async fn neighbors(
    State(db): State<Arc<Db>>,
    Path(id): Path<String>,
    Query(params): Query<NeighborsQuery>,
) -> Response {
    let depth = params.depth.unwrap_or(2);
    match db.get_neighbors(&id, depth).await {
        Ok(subgraph) => Json(subgraph).into_response(),
        Err(e) => api_error(e),
    }
}

fn api_error(e: DbError) -> Response {
    let (status, msg) = match &e {
        DbError::NotFound(m) => (StatusCode::NOT_FOUND, m.clone()),
        DbError::InvalidInput(m) => (StatusCode::BAD_REQUEST, m.clone()),
        DbError::Query(m) => (StatusCode::INTERNAL_SERVER_ERROR, m.clone()),
    };
    (status, Json(serde_json::json!({ "error": msg }))).into_response()
}
```

- [ ] **Step 2: Write `static/index.html`**

```html
<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<title>Knowledge Graph</title>
<script src="https://unpkg.com/vis-network@9/standalone/umd/vis-network.min.js"></script>
<style>
  body { font-family: system-ui, sans-serif; margin: 0; display: flex; flex-direction: column; height: 100vh; }
  #bar { padding: 12px; border-bottom: 1px solid #ddd; display: flex; gap: 8px; }
  #bar input { flex: 1; padding: 6px 10px; font-size: 14px; }
  #results { max-height: 160px; overflow-y: auto; border-bottom: 1px solid #ddd; }
  #results div { padding: 6px 12px; cursor: pointer; }
  #results div:hover { background: #f0f0f0; }
  #graph { flex: 1; }
</style>
</head>
<body>
  <div id="bar">
    <input id="search" type="text" placeholder="搜索知识点...">
  </div>
  <div id="results"></div>
  <div id="graph"></div>

<script>
const searchBox = document.getElementById('search');
const resultsBox = document.getElementById('results');
const graphBox = document.getElementById('graph');
let network = null;

async function doSearch() {
  const q = searchBox.value.trim();
  resultsBox.innerHTML = '';
  if (!q) return;
  const res = await fetch(`/api/concepts?q=${encodeURIComponent(q)}`);
  const concepts = await res.json();
  for (const c of concepts) {
    const row = document.createElement('div');
    row.textContent = `${c.label} [${c.category}] (掌握度 ${c.proficiency})`;
    row.onclick = () => showNeighbors(c.id);
    resultsBox.appendChild(row);
  }
}

async function showNeighbors(id) {
  const res = await fetch(`/api/neighbors/${encodeURIComponent(id)}?depth=2`);
  if (!res.ok) return;
  const sub = await res.json();

  const nodes = sub.nodes.map(n => ({ id: n.id, label: n.label }));
  const edges = sub.edges.map(e => ({ from: e.from, to: e.to, label: e.relation_type, arrows: 'to' }));

  if (network) network.destroy();
  network = new vis.Network(graphBox, { nodes, edges }, {
    physics: { solver: 'forceAtlas2Based' },
    nodes: { shape: 'dot', size: 14 },
    edges: { font: { size: 10 } },
  });
}

searchBox.addEventListener('keydown', (e) => { if (e.key === 'Enter') doSearch(); });
</script>
</body>
</html>
```

- [ ] **Step 3: Wire `web` into `src/main.rs`**

Update `src/main.rs`:

```rust
mod db;
mod tools;
mod web;

use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use axum::{Router, routing::get};
use rmcp::transport::streamable_http_server::{
    StreamableHttpServerConfig, StreamableHttpService, session::local::LocalSessionManager,
};

use db::Db;
use tools::KnowledgeGraphServer;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let data_dir = std::env::var("DATA_DIR").unwrap_or_else(|_| "./data".to_string());
    let port: u16 = std::env::var("PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(8787);

    let db = Arc::new(Db::open(&PathBuf::from(data_dir))?);

    let mcp_service = {
        let db = db.clone();
        StreamableHttpService::new(
            move || Ok(KnowledgeGraphServer::new(db.clone())),
            LocalSessionManager::default().into(),
            StreamableHttpServerConfig::default(),
        )
    };

    let router = Router::new()
        .route("/", get(web::index))
        .route("/api/concepts", get(web::list_concepts))
        .route("/api/neighbors/{id}", get(web::neighbors))
        .nest_service("/mcp", mcp_service)
        .with_state(db);

    let addr: SocketAddr = ([127, 0, 0, 1], port).into();
    let listener = tokio::net::TcpListener::bind(addr).await?;
    println!("listening on http://{addr}");
    axum::serve(listener, router).await?;
    Ok(())
}
```

- [ ] **Step 4: Build and manually verify end-to-end**

Run: `cargo build`
Expected: builds with no errors.

Run: `cargo run`, then in another terminal:
```bash
curl -s -X POST http://127.0.0.1:8787/mcp \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```
Then use the MCP session (or a real MCP client) to call `add_concept` once,
then:
```bash
curl -s 'http://127.0.0.1:8787/api/concepts?q=<part of the label you added>'
```
Expected: JSON array containing the concept you added via MCP — confirms the
REST API and the MCP tools share the same database.

Open `http://127.0.0.1:8787/` in a browser, search for the same keyword, and
confirm the result list shows up and clicking it renders a graph.

- [ ] **Step 5: Commit**

```bash
git add src/web.rs src/main.rs static/index.html
git commit -m "feat(web): read-only REST API + vis-network frontend"
```

---

## Task 7: README and MCP client configuration

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: nothing (documentation only)
- Produces: nothing (final task)

- [ ] **Step 1: Write `README.md`**

```markdown
# mcp-kuzu-knowledge

Local Rust MCP server backed by an embedded Kùzu graph database. Exposes 5
tools (`search_concepts`, `get_neighbors`, `add_concept`, `add_relation`,
`update_proficiency`) over MCP Streamable HTTP, plus a read-only web page to
browse the graph.

## Prerequisites

- Rust toolchain (`rustup`)
- `cmake` and a C++ toolchain (macOS: `brew install cmake`, `xcode-select --install`)
  — the first build compiles Kùzu's C++ core from source and takes several minutes.

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
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: usage and MCP client setup"
```
