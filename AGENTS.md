# opencode-openai-proxy

OpenAI Responses API → Chat Completions API 代理服务。将 Codex CLI 等客户端的 `/v1/responses` 请求转换为 Chat Completions 请求并转发到上游，同时将流式响应反向转换。

## Build / Run / Test

```bash
# 使用项目指定 Go 版本编译
GOROOT=/Users/xinglongliu/go/go1.25.8 /Users/xinglongliu/go/go1.25.8/bin/go build -o opencode-openai-proxy .

# 编译并启动
./manage.sh build
./manage.sh start
./manage.sh stop
./manage.sh restart
./manage.sh reopen   # SIGHUP 重新打开日志文件

# 直接运行（开发时）
GOROOT=/Users/xinglongliu/go/go1.25.8 /Users/xinglongliu/go/go1.25.8/bin/go run .

# 运行测试
GOROOT=/Users/xinglongliu/go/go1.25.8 /Users/xinglongliu/go/go1.25.8/bin/go test -v ./test/
GOROOT=/Users/xinglongliu/go/go1.25.8 /Users/xinglongliu/go/go1.25.8/bin/go test -v -run TestName ./test/

# 安装依赖
GOROOT=/Users/xinglongliu/go/go1.25.8 /Users/xinglongliu/go/go1.25.8/bin/go mod tidy
```

环境变量配置（见 `config/config.go`，支持 `.env` 文件）：
- `UPSTREAM_BASE_URL` — 默认上游地址，默认 `https://opencode.ai/zen`
- `UPSTREAM_ROUTES` — 按路径前缀分发的路由表，JSON 格式，如 `{"/v1/responses":"https://upstream-a.com","/v1":"https://upstream-b.com"}`。最长前缀匹配，未匹配时回退到 `UPSTREAM_BASE_URL`
- `LISTEN_ADDR` — 监听地址，默认 `:8082`
- `LOG_FILE` — 日志文件路径，默认 `./logs/proxy.log`
- `SEARCH_BACKEND` — 搜索后端：`bing` 或 `searxng`，默认 `searxng`
- `SEARXNG_BASE_URL` — SearXNG 地址（仅 `SEARCH_BACKEND=searxng` 时使用），默认 `http://localhost:8086`
- `SEARXNG_SUMMARIZE` — 是否将 SearXNG 搜索结果发给模型总结，默认 `false`
- `BLOCK_WEB_SEARCH` — 是否屏蔽 `web_search` 工具（设为 `true` 时不传 `web_search` 给上游模型，让 Codex CLI 自行处理搜索），默认 `true`

## 项目结构

```
.
├── main.go                 # 入口：Gin 路由设置、SIGHUP 日志重开
├── config/config.go        # 环境变量配置
├── converter/              # 协议转换核心逻辑
│   ├── request.go          # Responses API → Chat Completions 转换
│   └── response.go         # Chat Completions → Responses API 转换（含流式事件构建）
├── handler/                # HTTP 处理器
│   ├── responses.go        # /v1/responses 处理器（流式+非流式）
│   └── health.go           # /health 端点
├── middleware/             # Gin 中间件
│   ├── auth.go             # 认证（透传 Authorization，默认 Bearer public）
│   └── trace.go            # 追踪（trace_id 注入到 context 和响应头）
├── proxy/proxy.go          # 上游 HTTP 客户端（含路径路由选择）
├── logger/logger.go        # JSON 日志（文件+stderr 双写，支持 SIGHUP 重开）
├── searcher/               # 搜索后端
│   ├── searcher.go         # 搜索接口（支持后端切换）
│   ├── bing.go             # Bing 搜索实现
│   ├── searxng.go          # SearXNG 搜索实现
│   ├── fetcher.go          # 页面抓取
│   ├── search_test.go      # 搜索测试
│   └── searxng_test.go     # SearXNG 搜索测试
├── test/                   # 集成测试
│   ├── auth_test.go        # 认证中间件测试
│   └── responses_test.go   # 路径路由+SearXNG 集成测试
├── manage.sh               # 管理脚本
├── .env                    # 环境变量配置（gitignored）
└── logs/proxy.log          # 日志文件（gitignored）
```

## 代码风格指南

### 导入

```go
import (
    // 1. 标准库
    "encoding/json"
    "fmt"
    "net/http"

    // 2. 第三方库
    "github.com/gin-gonic/gin"
    "github.com/google/uuid"

    // 3. 项目内部包
    "github.com/LiuXingLong/opencode-openai-proxy/converter"
)
```

各块之间空行分隔，块内按字母序排序。不使用 `gofumpt` 等严格格式化工具。

### 命名

- **包名**：全小写，单单词（如 `converter`, `proxy`, `handler`）
- **公开类型/函数**：`PascalCase`（如 `NewStreamState`, `StreamToolCall`, `ChatCompletionsRequest`）
- **未导出类型/函数**：`camelCase`（如 `convertInput`, `inputItem`, `contentPart`）
- **JSON 标签**：始终使用 `snake_case`（如 `json:"tool_calls"`, `json:"output_index"`）
- **接口**：当前项目未使用接口，保持简单；需要时使用单方法命名惯例（如 `Handler`, `Parser`）
- **缩写**：保持全大写或全小写（如 `URL`, `ID`, `HTTP`, `respID`, `msgID`）

### 类型与结构体

```go
type MyStruct struct {
    Field    string          `json:"field,omitempty"`
    RawField json.RawMessage `json:"raw_field,omitempty"`
    Count    *int            `json:"count,omitempty"`  // 可空字段用指针
}
```

- 状态/可变结构体使用普通值
- 配置和一次性结构体使用指针
- `json.RawMessage` 用于需要透传的原始 JSON（如 tools, metadata）
- `map[string]interface{}` 用于动态构建 JSON（流式事件、响应构建）

### 错误处理

```go
// 错误包装使用 %w
if err != nil {
    return nil, fmt.Errorf("convert input: %w", err)
}

// 只在真正需要的地方 panic（如 mustJSON 中）
func mustJSON(v interface{}) json.RawMessage {
    b, err := json.Marshal(v)
    if err != nil {
        panic(err)
    }
    return b
}
```

- 所有错误必须处理或显式忽略（用 `_` 或注释说明）
- 流式处理中不影响逻辑的解析错误应静默忽略（返回空值），而非中断流程
- 使用 fmt.Errorf 而非 errors.New，始终用 `%w` 保留错误链

### 函数与方法

```go
// 构造函数用 New 前缀
func New(baseURL string) *Proxy { ... }
func NewStreamState(model string, createdAt int64) *StreamState { ... }

// 纯函数（无接收器），返回 string 而非 *string 表示必有值
func BuildStreamCreatedEvent(respID, model string, createdAt int64) string { ... }

// 方法接收器使用指针
func (s *StreamState) NextOutputIndex() int { ... }
```

- 函数按使用顺序排列：公开函数优先，辅助函数在后
- 复杂初始化放在构造函数中，不在调用方散落
- `must*` 前缀表示该函数在失败时 panic（只在初始化/序列化等确信不会失败的场景使用）

### 日志

```go
l := logger.FromContext(ctx)  // 从 context 获取带 trace_id 的 logger
l.Info("server starting", "addr", addr, "upstream", baseURL)
l.Error("upstream request failed", "error", err.Error())
```

- 使用 `log/slog` 结构化日志，key-value 对形式
- 所有 handler 通过 `logger.FromContext(ctx)` 获取 logger（自带 trace_id）
- 错误日志用 `l.Error`，常规信息用 `l.Info`，
- 日志中记录请求体/响应体用于调试，但注意不要记录敏感信息

### HTTP 处理

- 使用 Gin 框架，handler 接收 `*gin.Context`
- 路由注册在 `main.go` 中
- 中间件模式：`gin.HandlerFunc` 返回函数
- Auth/trace 通过 `c.Set`/`c.Get` 在中间件间传递数据

### 流式处理

- SSE 格式：`data: {json}\n\n`
- 流式状态管理使用 `StreamState` 结构体
- `output_index` 通过 `NextOutputIndex()` 递增分配，每个输出项只分配一次
- `item_id` 使用 `msg_`/`fcall_` 前缀 + UUID

### 注解

- 代码注释使用中文（如本文件所在项目的约定）
- 注释简洁，说明"为什么"而非"是什么"
- 结构体字段通常不需要注释，除非用途不直观
