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
    #[serde(default)]
    pub wikipedia_url: String,
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

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct DeleteConceptRequest {
    pub concept_id: String,
}

#[derive(Debug, Deserialize, schemars::JsonSchema)]
pub struct MergeConceptsRequest {
    pub keep_id: String,
    pub remove_id: String,
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

    #[tool(description = "Add a new concept node to the personal knowledge graph. `details` should be the user's own understanding/notes, not a full writeup — link canonical background via `wikipedia_url` instead of duplicating it.")]
    async fn add_concept(
        &self,
        Parameters(req): Parameters<AddConceptRequest>,
    ) -> Result<CallToolResult, McpError> {
        let concept = self
            .db
            .add_concept(
                &req.label,
                &req.category,
                req.proficiency,
                &req.details,
                &req.wikipedia_url,
            )
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

    #[tool(description = "Permanently delete a concept node and every RELATED_TO edge touching it. Use this to remove a genuinely wrong/unwanted node; to fix a DUPLICATE of an existing concept, prefer merge_concepts so the duplicate's edges aren't lost.")]
    async fn delete_concept(
        &self,
        Parameters(req): Parameters<DeleteConceptRequest>,
    ) -> Result<CallToolResult, McpError> {
        self.db
            .delete_concept(&req.concept_id)
            .await
            .map_err(db_err_to_mcp)?;
        Ok(CallToolResult::success(vec![ContentBlock::text("ok")]))
    }

    #[tool(description = "Merge a duplicate concept (`remove_id`) into the concept you want to keep (`keep_id`): every relation touching `remove_id` is re-created on `keep_id` (a relation that would become a self-loop on `keep_id` is dropped instead), then `remove_id` is deleted. Returns the surviving `keep_id` concept. Search first with search_concepts before add_concept to catch duplicates before they're created.")]
    async fn merge_concepts(
        &self,
        Parameters(req): Parameters<MergeConceptsRequest>,
    ) -> Result<CallToolResult, McpError> {
        let concept = self
            .db
            .merge_concepts(&req.keep_id, &req.remove_id)
            .await
            .map_err(db_err_to_mcp)?;
        json_result(&concept)
    }
}

#[tool_handler]
impl ServerHandler for KnowledgeGraphServer {
    fn get_info(&self) -> ServerInfo {
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_server_info(Implementation::new(
                env!("CARGO_PKG_NAME"),
                env!("CARGO_PKG_VERSION"),
            ))
            .with_protocol_version(ProtocolVersion::V_2024_11_05)
            .with_instructions(
                "Personal knowledge graph over a local Kùzu database. Tools: search_concepts, get_neighbors, add_concept, add_relation, update_proficiency, delete_concept, merge_concepts. Search before adding a concept to avoid creating duplicates; if a duplicate already exists, use merge_concepts rather than delete_concept so its relations aren't lost.".to_string(),
            )
    }
}
