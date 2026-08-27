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
    pub wikipedia_url: String,
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
        wikipedia_url: as_string(&row[5])?,
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
            "CREATE NODE TABLE Concept(id STRING PRIMARY KEY, label STRING, category STRING, proficiency INT64, details STRING, wikipedia_url STRING, created_at TIMESTAMP);",
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
        let trimmed = keyword.trim();
        let mut out = Vec::new();
        if trimmed.is_empty() {
            // Kùzu's `CONTAINS ''` evaluates false rather than matching
            // everything, so an empty keyword needs its own unfiltered
            // query instead of falling through to the CONTAINS-based one.
            let mut stmt = conn
                .prepare("MATCH (c:Concept) RETURN c.id, c.label, c.category, c.proficiency, c.details, c.wikipedia_url LIMIT 200;")?;
            let result = conn.execute(&mut stmt, vec![])?;
            for row in result {
                out.push(row_to_concept(&row)?);
            }
        } else {
            let mut stmt = conn.prepare(
                "MATCH (c:Concept) WHERE lower(c.label) CONTAINS lower($kw) OR lower(c.details) CONTAINS lower($kw) RETURN c.id, c.label, c.category, c.proficiency, c.details, c.wikipedia_url;",
            )?;
            let result =
                conn.execute(&mut stmt, vec![("kw", Value::String(keyword.to_string()))])?;
            for row in result {
                out.push(row_to_concept(&row)?);
            }
        }
        Ok(out)
    }

    pub async fn add_concept(
        &self,
        label: &str,
        category: &str,
        proficiency: i64,
        details: &str,
        wikipedia_url: &str,
    ) -> Result<Concept, DbError> {
        if !(0..=5).contains(&proficiency) {
            return Err(DbError::InvalidInput(
                "proficiency must be between 0 and 5".to_string(),
            ));
        }
        let id = Uuid::new_v4().to_string();
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(
            "CREATE (c:Concept {id: $id, label: $label, category: $category, proficiency: $proficiency, details: $details, wikipedia_url: $wikipedia_url, created_at: $created_at});",
        )?;
        conn.execute(
            &mut stmt,
            vec![
                ("id", Value::String(id.clone())),
                ("label", Value::String(label.to_string())),
                ("category", Value::String(category.to_string())),
                ("proficiency", Value::Int64(proficiency)),
                ("details", Value::String(details.to_string())),
                ("wikipedia_url", Value::String(wikipedia_url.to_string())),
                ("created_at", Value::Timestamp(OffsetDateTime::now_utc())),
            ],
        )?;
        Ok(Concept {
            id,
            label: label.to_string(),
            category: category.to_string(),
            proficiency,
            details: details.to_string(),
            wikipedia_url: wikipedia_url.to_string(),
        })
    }

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
            "MATCH (c:Concept {id: $id}) SET c.proficiency = $level RETURN c.id, c.label, c.category, c.proficiency, c.details, c.wikipedia_url;",
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

    pub async fn get_neighbors(&self, concept_id: &str, depth: i64) -> Result<Subgraph, DbError> {
        let depth = depth.clamp(1, 5);
        let conn = self.conn.lock().await;

        let mut center_stmt = conn.prepare(
            "MATCH (c:Concept {id: $id}) RETURN c.id, c.label, c.category, c.proficiency, c.details, c.wikipedia_url;",
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
            "MATCH (c:Concept {{id: $id}})-[:RELATED_TO*1..{depth}]-(n:Concept) RETURN DISTINCT n.id, n.label, n.category, n.proficiency, n.details, n.wikipedia_url;"
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
    async fn empty_keyword_search_lists_all_concepts() {
        let (_dir, db) = open_test_db();
        let a = db
            .add_concept("Rust Ownership", "language", 3, "borrow checker basics", "")
            .await
            .unwrap();
        let b = db
            .add_concept("Kùzu", "database", 1, "Embedded graph database", "")
            .await
            .unwrap();

        let found = db.search_concepts("").await.unwrap();
        assert_eq!(found.len(), 2);
        let ids: Vec<_> = found.iter().map(|c| c.id.clone()).collect();
        assert!(ids.contains(&a.id));
        assert!(ids.contains(&b.id));
    }

    #[tokio::test]
    async fn add_then_search_finds_by_label() {
        let (_dir, db) = open_test_db();
        let created = db
            .add_concept("Rust Ownership", "language", 3, "borrow checker basics", "")
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
        db.add_concept("Kùzu", "database", 1, "Embedded GRAPH database", "")
            .await
            .unwrap();

        let found = db.search_concepts("graph").await.unwrap();
        assert_eq!(found.len(), 1);
    }

    #[tokio::test]
    async fn add_concept_stores_and_returns_wikipedia_url() {
        let (_dir, db) = open_test_db();
        let created = db
            .add_concept(
                "Rust",
                "language",
                2,
                "my own notes",
                "https://en.wikipedia.org/wiki/Rust_(programming_language)",
            )
            .await
            .unwrap();
        assert_eq!(
            created.wikipedia_url,
            "https://en.wikipedia.org/wiki/Rust_(programming_language)"
        );

        let found = db.search_concepts("Rust").await.unwrap();
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].wikipedia_url, created.wikipedia_url);
    }

    #[tokio::test]
    async fn add_concept_rejects_out_of_range_proficiency() {
        let (_dir, db) = open_test_db();
        let err = db.add_concept("X", "cat", 9, "details", "").await.unwrap_err();
        assert!(matches!(err, DbError::InvalidInput(_)));
    }

    #[tokio::test]
    async fn add_relation_links_two_existing_concepts() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "", "").await.unwrap();

        db.add_relation(&a.id, &b.id, "depends_on").await.unwrap();
    }

    #[tokio::test]
    async fn add_relation_errors_on_missing_concept() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();

        let err = db
            .add_relation(&a.id, "does-not-exist", "depends_on")
            .await
            .unwrap_err();
        assert!(matches!(err, DbError::NotFound(_)));
    }

    #[tokio::test]
    async fn update_proficiency_changes_value() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();

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
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();
        let err = db.update_proficiency(&a.id, 9).await.unwrap_err();
        assert!(matches!(err, DbError::InvalidInput(_)));
    }

    #[tokio::test]
    async fn get_neighbors_includes_center_and_direct_edge() {
        let (_dir, db) = open_test_db();
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "", "").await.unwrap();
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
        let a = db.add_concept("A", "cat", 0, "", "").await.unwrap();
        let b = db.add_concept("B", "cat", 0, "", "").await.unwrap();
        let c = db.add_concept("C", "cat", 0, "", "").await.unwrap();
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
}
