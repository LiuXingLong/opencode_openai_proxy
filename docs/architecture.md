# opencode-openai-proxy 架构图

## 一、整体系统架构图

```mermaid
graph TB
    subgraph 客户端[客户端]
        CLI[Codex CLI / @ai-sdk/openai]
    end

    subgraph 代理[opencode-openai-proxy]
        direction TB

        subgraph 入口层
            GIN[Gin Router]
            MID_TRACE[Trace 中间件<br/>trace_id 注入]
            MID_AUTH[Auth 中间件<br/>认证透传]
        end

        subgraph 核心处理层
            HANDLER[ResponsesHandler<br/>请求编排总入口]
        end

        subgraph 协议转换层
            REQ_CONV[converter/request.go<br/>Responses API → Chat Completions]
            RESP_CONV[converter/response.go<br/>Chat Completions → Responses API<br/>+ SSE 事件构建]
        end

        subgraph 代理转发层
            PROXY[proxy.Proxy<br/>路由选择 + HTTP 转发]
        end

        subgraph 搜索集成层
            SEARCHER[Searcher<br/>统一搜索接口]
            BING[Bing 后端<br/>HTML 解析 + 页面抓取]
            SEARXNG[SearXNG 后端<br/>JSON API 调用]
            FETCHER[页面抓取器<br/>并发 + goquery 解析]
        end

        subgraph 基础服务层
            CONFIG[config<br/>环境变量配置]
            LOGGER[logger<br/>JSON 结构化日志]
        end
    end

    subgraph 上游[上游服务]
        UPSTREAM_A[opencode.ai/zen]
        UPSTREAM_B[Ollama 本地]
        UPSTREAM_C[其他 Chat API]
    end

    CLI -->|"POST /v1/responses<br/>JSON"| GIN
    GIN --> MID_TRACE --> MID_AUTH --> HANDLER
    HANDLER -->|"转换请求"| REQ_CONV
    REQ_CONV -->|"Chat Completions JSON"| PROXY
    PROXY -->|"POST /v1/chat/completions"| UPSTREAM_A
    PROXY -->|"POST /v1/chat/completions"| UPSTREAM_B
    PROXY -->|"POST /v1/chat/completions"| UPSTREAM_C
    UPSTREAM_A -->|"JSON / SSE"| PROXY
    UPSTREAM_B -->|"JSON / SSE"| PROXY
    UPSTREAM_C -->|"JSON / SSE"| PROXY
    PROXY -->|"原始响应"| HANDLER
    HANDLER -->|"转换响应"| RESP_CONV
    RESP_CONV -->|"Responses API 格式<br/>JSON / SSE"| CLI

    HANDLER -.->|"web_search 触发"| SEARCHER
    SEARCHER -.-> BING
    SEARCHER -.-> SEARXNG
    BING -.-> FETCHER
    SEARCHER -.->|"搜索结果"| HANDLER
    HANDLER -.->|"搜索结果重请求"| PROXY

    CONFIG -.-> HANDLER
    CONFIG -.-> PROXY
    LOGGER -.-> HANDLER
    LOGGER -.-> PROXY
    LOGGER -.-> SEARCHER
```

---

## 二、模块依赖关系图

```mermaid
graph TD
    MAIN["main.go<br/>入口：路由注册、初始化"] --> CONFIG["config<br/>配置加载"]
    MAIN --> LOGGER["logger<br/>日志系统"]
    MAIN --> MIDDLEWARE["middleware<br/>Trace + Auth"]
    MAIN --> HANDLER["handler<br/>ResponsesHandler"]
    MAIN --> PROXY["proxy<br/>HTTP 转发"]
    MAIN --> SEARCHER["searcher<br/>搜索集成"]

    MIDDLEWARE --> LOGGER

    HANDLER --> CONVERTER["converter<br/>协议转换"]
    HANDLER --> PROXY
    HANDLER --> SEARCHER
    HANDLER --> MIDDLEWARE
    HANDLER --> LOGGER

    PROXY --> LOGGER

    SEARCHER --> LOGGER
    SEARCHER --> BING["bing.go<br/>依赖 goquery"]
    SEARCHER --> SEARXNG["searxng.go"]
    SEARCHER --> FETCHER["fetcher.go<br/>依赖 goquery"]

    CONVERTER --> REQ_CONV["request.go<br/>Responses → Chat"]
    CONVERTER --> RESP_CONV["response.go<br/>Chat → Responses"]
```

---

## 三、请求响应数据流图

### 3.1 非流式请求数据流

```mermaid
flowchart LR
    subgraph 入站
        RAW[原始请求体<br/>JSON bytes]
    end

    subgraph 转换
        REQ[ResponsesRequest<br/>结构体解析]
        MSG[[]Message<br/>消息数组]
        CHAT[ChatCompletionsRequest<br/>JSON bytes]
    end

    subgraph 转发
        HTTP[HTTP POST<br/>上游响应]
    end

    subgraph 响应处理
        CHECK{"检测<br/>web_search<br/>tool_call?"}
        SEARCH_PROC[搜索处理<br/>SearXNG / Bing]
        CONV_RESP[ConvertNonStreamingResponse<br/>Chat → Responses]
        FINAL[最终响应体<br/>Responses API JSON]
    end

    RAW -->|json.Unmarshal| REQ
    REQ -->|convertInput| MSG
    REQ -->|convertTools| MSG
    MSG -->|json.Marshal| CHAT
    CHAT -->|Proxy.Send| HTTP
    HTTP -->|io.ReadAll| CHECK
    CHECK -->|无工具调用| CONV_RESP
    CHECK -->|有 web_search| SEARCH_PROC
    CONV_RESP --> FINAL
    SEARCH_PROC --> FINAL
```

### 3.2 流式数据流

```mermaid
flowchart LR
    subgraph 上游[上游 SSE 流]
        LINE["data: {...} 行"]
        DONE["data: [DONE]"]
    end

    subgraph 解析层[ParseChatStreamLine]
        DELTA[delta.content]
        TOOL[delta.tool_calls]
        FINISH[finish_reason]
        USAGE[usage]
    end

    subgraph 事件构建[StreamState + 事件函数]
        CREATED["response.created"]
        ITEM_ADDED["response.output_item.added"]
        TEXT_DELTA["response.output_text.delta"]
        TEXT_DONE["response.output_text.done"]
        FC_DELTA["response.function_call_arguments.delta"]
        FC_DONE["response.function_call_arguments.done"]
        ITEM_DONE["response.output_item.done"]
        COMPLETED["response.completed"]
    end

    subgraph 客户端[Client SSE 流]
        OUT[SSE 事件序列]
    end

    LINE --> DELTA
    LINE --> TOOL
    LINE --> FINISH
    LINE --> USAGE

    DELTA -.->|有 content| TEXT_DELTA
    DELTA -.->|首个 delta| ITEM_ADDED
    FINISH -.-> TEXT_DONE
    FINISH -.-> ITEM_DONE
    FINISH -.-> COMPLETED
    TOOL -.->|首个出现| ITEM_ADDED
    TOOL -.->|参数增量| FC_DELTA
    TOOL -.->|完成| FC_DONE
    TOOL -.->|完成| ITEM_DONE

    USAGE -.-> COMPLETED
    DONE -->|跳出循环| COMPLETED

    CREATED --> OUT
    ITEM_ADDED --> OUT
    TEXT_DELTA --> OUT
    TEXT_DONE --> OUT
    FC_DELTA --> OUT
    FC_DONE --> OUT
    ITEM_DONE --> OUT
    COMPLETED --> OUT
```

---

## 四、搜索架构图

```mermaid
graph TB
    SEARCH_TRIGGER["上游响应含 web_search tool_call"]

    SEARCH_TRIGGER --> CHECK_BLOCK{"BLOCK_WEB_SEARCH<br/>是否 true?"}

    CHECK_BLOCK -->|true| SKIP["跳过 web_search<br/>客户端自行处理<br/>= tools 置 nil"]
    CHECK_BLOCK -->|false| CHECK_BACKEND{"SEARCH_BACKEND<br/>选择"}

    CHECK_BACKEND -->|searxng| SEARXNG_BRANCH
    CHECK_BACKEND -->|bing| BING_BRANCH

    subgraph SEARXNG_BRANCH[SearXNG 搜索流程]
        SEARX_API["GET /search?q=X&format=json<br/>调用 SearXNG JSON API"]
        SEARX_RESULT[解析 results 数组<br/>直接获取结构化结果]
        CHECK_SUMMARIZE{"SEARXNG_SUMMARIZE<br/>是否 true?"}
        DIRECT_FMT["直接格式化结果文本<br/>按序号排列"]
        MODEL_SUMMARIZE["BuildReInvokeRequest<br/>模型总结搜索结果"]
        BUILD_SEARXNG_RESP["buildSearXNGSearchResponse<br/>构建 web_search_call + message"]
    end

    subgraph BING_BRANCH[Bing 搜索流程]
        BING_SCRAPE["searchBing<br/>请求 Bing SERP HTML"]
        BING_PARSE["goquery 解析<br/>提取 .b_algo 结果"]
        BING_SERP["提取 SERP 正文<br/>去噪音作为虚拟结果"]
        FETCH_PAGES["fetchPages<br/>并发抓取结果页面"]
        FILTER["过滤无页面内容项"]
        RETRY_LOOP["重试循环<br/>最多 retryCount+1 次"]
        BUILD_REINVOKE["BuildReInvokeRequest<br/>搜索结果 + 原始问题"]
        SEND_REINVOKE["Proxy.Send<br/>模型总结"]
        CHECK_RESULT{"回答有效<br/>非 SEARCH_RESULT<br/>_INSUFFICIENT?"}
        BUILD_BING_RESP["buildSearchResponse<br/>构建含文本的响应"]
        FALLBACK["fallback 消息"]
    end

    SEARX_API --> SEARX_RESULT
    SEARX_RESULT --> CHECK_SUMMARIZE
    CHECK_SUMMARIZE -->|false| DIRECT_FMT
    CHECK_SUMMARIZE -->|true| MODEL_SUMMARIZE
    DIRECT_FMT --> BUILD_SEARXNG_RESP
    MODEL_SUMMARIZE --> BUILD_SEARXNG_RESP

    BING_SCRAPE --> BING_PARSE --> BING_SERP --> FETCH_PAGES --> FILTER
    FILTER --> RETRY_LOOP
    RETRY_LOOP --> BUILD_REINVOKE --> SEND_REINVOKE --> CHECK_RESULT
    CHECK_RESULT -->|是| BUILD_BING_RESP
    CHECK_RESULT -->|否| RETRY_LOOP
    RETRY_LOOP -->|"全部耗尽"| FALLBACK
    FALLBACK --> BUILD_BING_RESP
```

---

## 五、路径路由架构图

```mermaid
graph TB
    REQ["请求路径<br/>如 /ollama/v1/responses"]

    REQ --> ROUTE["Proxy.selectBaseURL(path)<br/>最长前缀匹配"]

    subgraph 路由表[UPSTREAM_ROUTES 配置]
        R1["/v1/responses<br/>→ opencode.ai/zen"]
        R2["/ollama/v1/responses<br/>→ http://localhost:11434"]
        R3["/ollama/v1<br/>→ http://localhost:11434"]
    end

    subgraph 回退[默认上游]
        DEFAULT["UPSTREAM_BASE_URL<br/>→ opencode.ai/zen"]
    end

    ROUTE --> R1
    ROUTE --> R2
    ROUTE --> R3
    ROUTE -->|未匹配| DEFAULT

    R1 --> UP_URL1["/v1/chat/completions"]
    R2 --> UP_URL2["/v1/chat/completions"]
    R3 --> UP_URL3["/v1/chat/completions"]
    DEFAULT --> UP_URL4["/v1/chat/completions"]
```

---

## 六、StreamState 输出索引分配图

```mermaid
sequenceDiagram
    participant Stream as StreamState
    participant Handler as ResponsesHandler
    participant Client as 客户端

    Note over Stream: nextOutputIndex=0

    Handler->>Stream: tool_call index=0 首次到来
    Stream->>Stream: NextOutputIndex() → 0
    Stream-->>Handler: output_index=0
    Handler->>Client: response.output_item.added (web_search_call)

    Handler->>Stream: tool_call index=1 首次到来
    Stream->>Stream: NextOutputIndex() → 1
    Stream-->>Handler: output_index=1
    Handler->>Client: response.output_item.added (get_weather)

    Handler->>Stream: 首个 content delta 到来
    Stream->>Stream: NextOutputIndex() → 2
    Stream-->>Handler: output_index=2
    Handler->>Client: response.output_item.added (message)
    Handler->>Client: response.output_text.delta (同一 output_index=2)

    Handler->>Stream: finish_reason → 搜索触发
    Stream->>Stream: NextOutputIndex() → 3
    Stream-->>Handler: output_index=3
    Handler->>Client: response.output_item.added (搜索结果 message)
    Handler->>Client: response.output_text.delta (同一 output_index=3)
```

---

## 七、协议转换映射图

### 7.1 请求映射（Responses → Chat Completions）

```mermaid
flowchart LR
    subgraph 左[Responses API]
        IN["input<br/>(string/array)"]
        INS["instructions"]
        MAX["max_output_tokens"]
        TOOLS["tools<br/>web_search/function"]
        TEXT["text.format"]
        STREAM["stream"]
        OTHER["temperature/top_p/..."]
    end

    subgraph 右[Chat Completions]
        MSGS["messages<br/>[{role,content}...]"]
        MAXTOK["max_tokens"]
        FUNCS["tools<br/>{type:function,...}"]
        FMT["response_format"]
        ST["stream"]
        OTHERS["全部透传"]
    end

    IN -->|"convertInput()"| MSGS
    INS -->|"头部插入 system"| MSGS
    MAX --> MAXTOK
    TOOLS -->|"convertTools()"| FUNCS
    TEXT --> FMT
    STREAM --> ST
    OTHER --> OTHERS
```

### 7.2 响应映射（Chat Completions → Responses）

```mermaid
flowchart LR
    subgraph 上游[Chat 响应]
        CHAT_ID["id"]
        CHAT_CONTENT["choices[0].message.content"]
        CHAT_TOOL["choices[0].message.tool_calls"]
        CHAT_FINISH["choices[0].finish_reason"]
        CHAT_USAGE["usage"]
    end

    subgraph 下游[Responses 响应]
        RESP_ID["id<br/>resp_UUID 新生成"]
        OUTPUT["output[]"]
        RSTATUS["status"]
        RUSAGE["usage"]
    end

    CHAT_ID -->|"丢弃"| RESP_ID
    CHAT_CONTENT -->|"output_text"| OUTPUT
    CHAT_TOOL -->|"web_search → web_search_call<br/>其他 → function_call"| OUTPUT
    CHAT_FINISH -->|"MapFinishReason()"| RSTATUS
    CHAT_USAGE -->|"convertUsage()"| RUSAGE
```
