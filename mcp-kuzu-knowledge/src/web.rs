use std::sync::Arc;

use axum::{
    Json,
    extract::{Path, Query, State},
    http::{StatusCode, header},
    response::{Html, IntoResponse, Response},
};
use serde::Deserialize;

use crate::db::{Db, DbError};

const INDEX_HTML: &str = include_str!("../static/index.html");
const VIS_NETWORK_JS: &str = include_str!("../static/vis-network.min.js");

pub async fn index() -> Html<&'static str> {
    Html(INDEX_HTML)
}

pub async fn vis_network_js() -> impl IntoResponse {
    ([(header::CONTENT_TYPE, "application/javascript")], VIS_NETWORK_JS)
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

pub async fn full_graph(State(db): State<Arc<Db>>) -> Response {
    match db.get_full_graph().await {
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
