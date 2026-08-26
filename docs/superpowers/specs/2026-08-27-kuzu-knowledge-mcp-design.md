# Kùzu 知识图谱 MCP Server — 设计文档

日期：2026-08-27

## 背景与目标

本地跑一个 Kùzu（嵌入式图数据库）后端，管理个人知识/学习笔记图谱，通过 MCP
（Model Context Protocol）把增删查改能力暴露给本地的 Claude Code 和 Gemini
CLI，让它们在回答问题前可以先查询相关知识点上下文（简化版 GraphRAG）。
同时提供一个极简的只读网页前端，方便人眼直接浏览图谱。

项目独立存放，不属于 `ai_learn` 仓库内的代码，建议路径：
`~/mcp-kuzu-knowledge/`（单独 git 仓库或本地目录，由用户决定是否纳入版本控制）。

## 技术栈

- 语言：Rust
- 图数据库：[`kuzu`](https://crates.io/crates/kuzu) crate（官方 Rust 绑定，嵌入式，直接打开本地目录）
- MCP：[`rmcp`](https://github.com/modelcontextprotocol/rust-sdk)（官方 Rust SDK），使用 Streamable HTTP transport
- HTTP/路由：`rmcp` 底层基于 axum，直接复用同一个 axum Router 加自定义路由
- 前端：单个静态 HTML 文件（`include_str!` 打包进二进制），vanilla JS + [vis-network](https://visjs.github.io/vis-network/)（CDN 引入）

## 架构

```
~/mcp-kuzu-knowledge/
├── Cargo.toml
├── src/
│   ├── main.rs        # 启动 HTTP server（axum），挂载 /mcp、/api/*、/
│   ├── db.rs          # kuzu 连接、schema 初始化、查询封装
│   └── tools.rs       # 5 个 MCP 工具的实现（rmcp #[tool] 宏）
├── static/
│   └── index.html     # 只读前端页面（搜索 + 力导向图）
├── run.sh             # 手动启动脚本
├── README.md          # 使用说明：如何启动、如何在 CC/Gemini 里配置 MCP endpoint
├── .gitignore          # 忽略 data/、target/
└── data/               # kuzu 数据库文件目录，运行时生成
```

单进程、单端口（默认 `127.0.0.1:8787`，可通过环境变量覆盖）：

- `POST /mcp` — MCP Streamable HTTP endpoint，供 Claude Code / Gemini CLI 连接
- `GET /api/concepts?q=<keyword>` — 搜索/列出 concept，返回 JSON 数组
- `GET /api/neighbors/:id?depth=<n>` — 返回以 `:id` 为中心、深度 `depth`（默认 2）的子图 JSON（`nodes` + `edges`）
- `GET /` — 返回静态只读前端页面

Kùzu 数据库以嵌入式方式仅被这一个进程打开，所有读写（无论来自 MCP 工具调用还是
`/api/*` 只读接口）都在同一进程内用 `tokio::sync::Mutex<Database>`（或
`Connection`）串行化，避免并发写冲突；`/api/*` 路径本身只读，不需要写锁但共享同一把
互斥锁读取即可，无需额外一致性设计。

前端与 MCP 完全解耦：前端只调 `/api/*` 只读接口，不经过 MCP 协议；CC/Gemini 只
通过 `/mcp` 调用工具。两者共享同一份底层数据。

## 数据模型（Schema）

Kùzu 建两张表，先保持通用、不过早细分关系类型：

```cypher
CREATE NODE TABLE Concept(
  id STRING PRIMARY KEY,
  label STRING,
  category STRING,
  proficiency INT64,      -- 掌握程度，0-5
  details STRING,
  created_at TIMESTAMP
)

CREATE REL TABLE RELATED_TO(
  FROM Concept TO Concept,
  relation_type STRING     -- 自由字符串，如 "depends_on" / "similar_to" / "part_of"
)
```

- `id` 由 server 生成（UUID v4 字符串），调用方不需要指定
- 首次启动时若表不存在则自动建表（幂等的 schema 初始化逻辑写在 `db.rs`）
- 数据库从空状态开始，不做批量导入；后续通过 `add_concept` / `add_relation` 在
  日常对话中逐步积累

## MCP 工具

在 `tools.rs` 中用 `rmcp` 的 `#[tool]` 宏定义以下 5 个工具，参数通过带
`Deserialize` + `JsonSchema` 的 struct 声明：

1. `search_concepts(keyword: String) -> Vec<ConceptSummary>`
   按 `label` / `details` 做子串模糊匹配（Kùzu 的 `CONTAINS` 或类似字符串函数）
2. `get_neighbors(concept_id: String, depth: Option<u32> = 2) -> Subgraph`
   从指定节点出发做图遍历，返回 `{ nodes: [...], edges: [...] }`
3. `add_concept(label: String, category: String, proficiency: i64, details: String) -> ConceptSummary`
   新增节点，`id` 内部生成，`created_at` 用当前时间
4. `add_relation(from_id: String, to_id: String, relation_type: String) -> ()`
   建立一条 `RELATED_TO` 边；若 `from_id` 或 `to_id` 不存在，返回工具错误
5. `update_proficiency(concept_id: String, new_level: i64) -> ConceptSummary`
   更新指定节点的 `proficiency`；`new_level` 校验范围 0-5，越界返回工具错误；
   `concept_id` 不存在同样返回工具错误

`ConceptSummary` 是一个统一的返回结构：`{ id, label, category, proficiency, details }`。

## 前端（只读）

`static/index.html` 单文件，内容：

- 顶部搜索框：输入关键字，实时（或回车）调用 `GET /api/concepts?q=`，下方列出匹配的
  concept（label + category + proficiency）
- 点击某一条结果：调用 `GET /api/neighbors/:id?depth=2`，用 vis-network 画一张
  力导向图；节点标签显示 `label`，边标签显示 `relation_type`
- 纯展示，没有任何编辑/表单交互；增删改一律通过 MCP 工具（CC/Gemini 对话）完成
- vis-network 通过 `<script src="https://unpkg.com/vis-network/...">` 走 CDN 引入
  （本地工具场景，非 Claude Artifact，无 CSP 限制）

## 错误处理

- MCP 工具遇到"节点不存在"等业务错误 → 返回 MCP tool error（带清晰文字），不是
  HTTP 500，让调用它的 LLM 能读懂并向用户解释，而不是进程崩溃
- Kùzu 查询/连接异常 → 在 `db.rs` 里统一捕获，转换成工具错误或 `/api/*` 的
  4xx/5xx JSON 错误体（`{ "error": "..." }"`），不允许 panic 导致整个 server 退出
- `update_proficiency` 的越界输入（<0 或 >5）在工具层做参数校验，提前拒绝

## 启动方式

- 手动启动：`cargo run --release`（或 `run.sh` 包一层，读取 `PORT` 环境变量，默认 8787）
- `README.md` 写清楚：
  - 如何启动 server
  - 如何在 Claude Code 的 MCP 配置里加一个 `type: "http"` / `url: "http://127.0.0.1:8787/mcp"` 的 server 条目
  - 如何在 Gemini CLI 里做等价配置（若版本支持 HTTP transport；若不支持，退化为 stdio 的说明放在 README 的已知限制里）
  - 浏览器打开 `http://127.0.0.1:8787/` 看图
- 不做开机自启（launchd），后续需要再加

## 测试

- Rust 单元测试（`#[cfg(test)]`）：对 `db.rs` 里的每个查询函数，用临时目录起一个
  独立 Kùzu 数据库实例，验证：
  - `add_concept` 后能被 `search_concepts` 搜到
  - `add_relation` 后 `get_neighbors` 能返回对应边
  - `update_proficiency` 正确更新数值，越界输入被拒绝
  - 查询不存在的 `concept_id` 返回预期错误
- 手动验证（集成层面）：
  - 启动 server，用 `curl` 或浏览器验证 `/api/concepts`、`/api/neighbors/:id`
  - 在 Claude Code 里配置好 MCP endpoint 后，实际对话中调用一次
    `add_concept` + `get_neighbors`，确认端到端可用
  - 浏览器打开首页，搜索 + 点开图，确认可视化正常

## 已知限制 / 后续可扩展项（本次不做）

- 不做鉴权（仅绑定 `127.0.0.1`，同机可信）
- 不做开机自启动（launchd），需要时后续单独加
- 不做批量导入脚本（从零开始积累）
- relation_type 目前是自由字符串，不做枚举校验；后续若发现固定模式再考虑拆分成多张
  REL TABLE 或加校验
- 前端不支持编辑；如后续想要可读可写前端，需要新的设计迭代
