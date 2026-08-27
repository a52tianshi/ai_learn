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
