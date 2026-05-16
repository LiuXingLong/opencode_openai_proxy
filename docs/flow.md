# opencode-openai-proxy 流程图

## 一、请求总流程图

```mermaid
flowchart TB
    START(["客户端请求<br/>POST /v1/responses"]) --> TRACE["Trace 中间件<br/>生成 trace_id 注入 context"]
    TRACE --> AUTH["Auth 中间件<br/>提取/兜底 Authorization"]
    AUTH --> READ["读取请求体<br/>io.ReadAll"]
    READ --> PARSE_META["解析 model + stream"]
    PARSE_META --> CONVERT_REQ["ConvertRequest<br/>Responses → Chat Completions"]
    CONVERT_REQ --> ROUTE["Proxy.selectBaseURL<br/>最长前缀路由匹配"]
    ROUTE --> SEND["Proxy.Send<br/>POST /v1/chat/completions"]
    SEND --> STATUS_CHECK{"上游状态码<br/>== 200?"}

    STATUS_CHECK -->|否| ERROR_RESP["透传上游错误响应<br/>c.Data(statusCode)"]
    STATUS_CHECK -->|是| STREAM_CHECK{"stream<br/>== true?"}

    STREAM_CHECK -->|是| HANDLE_STREAM["handleStreaming<br/>流式处理"]
    STREAM_CHECK -->|否| HANDLE_NONSTREAM["handleNonStreaming<br/>非流式处理"]

    HANDLE_STREAM ----> END(["响应客户端"])
    HANDLE_NONSTREAM ----> END
    ERROR_RESP ----> END
```

---

## 二、非流式处理流程图

```mermaid
flowchart TB
    START(["handleNonStreaming<br/>非流式入口"]) --> READ_BODY["读取上游完整响应<br/>io.ReadAll"]
    READ_BODY --> DETECT["extractWebSearchToolCall<br/>检测 web_search tool_call"]
    DETECT --> HAS_SEARCH{"检测到<br/>web_search?"

    HAS_SEARCH -->|否| CONV_RESP["ConvertNonStreamingResponse<br/>直接转换"]
    CONV_RESP --> RETURN["c.Data 返回 JSON"]

    HAS_SEARCH -->|"是"| BLOCK_CHECK{"BLOCK_WEB_SEARCH<br/>= false?"}

    BLOCK_CHECK -->|false| CONV_RESP
    BLOCK_CHECK -->|true| BACKEND_CHECK{"SEARCH_BACKEND<br/>后端类型?"}

    BACKEND_CHECK -->|searxng| SEARXNG_NS[handleSearXNGNonStreaming]
    BACKEND_CHECK -->|bing| BING_NS[Bing 搜索 + 重试]

    SEARXNG_NS --> RETURN
    BING_NS --> RETURN
```

### 2.1 SearXNG 非流式子流程

```mermaid
flowchart TB
    START(["handleSearXNGNonStreaming"]) --> SEARCH["Searcher.Search(ctx, query)<br/>调用 SearXNG JSON API"]
    SEARCH --> CHECK_RESULTS{"有结果?"}
    CHECK_RESULTS -->|否| BUILD_EMPTY["构建空结果响应"]
    CHECK_RESULTS -->|是| CHECK_SUMM{"SEARXNG_SUMMARIZE<br/>= true?"}

    CHECK_SUMM -->|否| FMT["formatSearchResults<br/>直接格式化"]
    CHECK_SUMM -->|是| SUMMARIZE["BuildReInvokeRequest<br/>模型总结搜索结果"]
    SUMMARIZE --> SEND_REINVOKE["Proxy.Send(非流式)"]
    SEND_REINVOKE --> EXTRACT["extractContentFromResponse<br/>提取总结文本"]
    EXTRACT --> FMT

    FMT --> BUILD["buildSearXNGSearchResponse"]
    BUILD_EMPTY --> BUILD
    BUILD --> RESP["返回含 web_search_call<br/>+ message 的 JSON"]
```

### 2.2 Bing 非流式 + 重试子流程

```mermaid
flowchart TB
    START(["Bing 搜索 + 重试"]) --> SEARCH["Searcher.Search(ctx, query)"]

    subgraph BING_SEARCH[Bing 搜索过程]
        BING_SCRAPE["searchBing<br/>请求 Bing SERP HTML"]
        BING_PARSE["goquery 解析<br/>提取 .b_algo 标题/URL/摘要"]
        BING_SERP["提取 SERP 正文<br/>去噪音后作为结果"]
        FETCH["fetchPages<br/>并发抓取 + goquery 解析"]
        FILTER["过滤无页面内容项"]
    end

    SEARCH --> BING_SCRAPE --> BING_PARSE --> BING_SERP --> FETCH --> FILTER
    FILTER --> CHECK_FILTER{"过滤后有<br/>结果?"}

    CHECK_FILTER -->|否| BUILD_EMPTY["buildSearchResponse<br/>空文本"]
    CHECK_FILTER -->|是| RETRY_INIT["初始化重试计数器<br/>attempt=0"]

    RETRY_INIT --> RETRY_CHECK{"attempt <=<br/>retryCount?"}

    RETRY_CHECK -->|否| RETRY_EXHAUSTED["全部重试耗尽"]
    RETRY_CHECK -->|是| BUILD_REQ["BuildReInvokeRequest<br/>搜索结果 JSON + 原始请求"]
    BUILD_REQ --> SEND_REQ["Proxy.Send<br/>发送模型总结请求"]
    SEND_REQ --> CHECK_REQ_RESP{"状态码 200?"}
    CHECK_REQ_RESP -->|否| INC_RETRY["attempt++"]
    CHECK_REQ_RESP -->|是| EXTRACT["extractContentFromResponse"]
    EXTRACT --> CHECK_VALID{"文本非空 且<br/>不含 SEARCH_RESULT<br/>_INSUFFICIENT?"}

    CHECK_VALID -->|是| ACCEPT["接受结果"]
    CHECK_VALID -->|否| INC_RETRY

    INC_RETRY --> RETRY_CHECK

    ACCEPT --> BUILD["buildSearchResponse<br/>含最终文本"]
    RETRY_EXHAUSTED --> BUILD_FALLBACK["buildSearchResponse<br/>fallback 空文本"]
    BUILD_EMPTY --> BUILD
    BUILD --> RESP["返回含文本的 JSON"]
```

---

## 三、流式处理流程图

### 3.1 流式主流程

```mermaid
flowchart TB
    START(["handleStreaming"]) --> STATE["初始化 StreamState<br/>respID, createdAt"]
    STATE --> SET_HEADER["设置 SSE Headers<br/>Content-Type: text/event-stream"]
    SET_HEADER --> SEND_CREATED["发送<br/>response.created"]
    SEND_CREATED --> SCANNER["bufio.Scanner<br/>逐行读取上游 SSE"]

    SCANNER --> PARSE_LINE["ParseChatStreamLine<br/>解析 data: {...} 行"]

    PARSE_LINE --> IS_DONE{"isDone<br/>= true?"}
    IS_DONE -->|是| SCAN_ERR{"scanner.Err()<br/>检查"}

    IS_DONE -->|否| HAS_TOOL{"有 tool_call<br/>delta?"}
    HAS_TOOL -->|是| HANDLE_TOOL["处理工具调用<br/>分配索引 + 事件发送"]
    HANDLE_TOOL --> CONTINUE_TOOL[继续]
    HAS_TOOL -->|否| CONTINUE_TOOL

    CONTINUE_TOOL --> HAS_TEXT{"有 content<br/>delta?"}
    HAS_TEXT -->|是| HANDLE_TEXT["处理文本<br/>ItemAdded? + delta 事件"]
    HANDLE_TEXT --> CONTINUE_TEXT[继续]
    HAS_TEXT -->|否| CONTINUE_TEXT

    CONTINUE_TEXT --> HAS_USAGE{"有 usage?"}
    HAS_USAGE -->|是| UPDATE_USAGE["更新 PromptTokens<br/>CompletionTokens"]
    UPDATE_USAGE --> CONTINUE_USAGE[继续]
    HAS_USAGE -->|否| CONTINUE_USAGE

    CONTINUE_USAGE --> HAS_FINISH{"有 finish_reason?"}
    HAS_FINISH -->|否| SCANNER
    HAS_FINISH -->|是| HANDLE_FINISH["处理 finish_reason"]

    HANDLE_FINISH --> AGG_FC["聚合 tool_calls 参数<br/>mergeMap 去重合并"]

    AGG_FC --> CHECK_FC{"存在 tool_calls?"}
    CHECK_FC -->|是| SEND_FC_DONE["发送非内置工具的<br/>function_call_arguments.done"]
    SEND_FC_DONE--> SEND_ITEM_DONE_FC["发送各 tool_call 的<br/>output_item.done"]
    SEND_ITEM_DONE_FC --> CHECK_ALL_WEB{"全部为<br/>web_search?"}
    CHECK_FC -->|否| SKIP_FC[跳过]

    CHECK_ALL_WEB -->|是| ALL_WEB_SEARCH["进入搜索流程"]
    CHECK_ALL_WEB -->|否| SKIP_SEARCH[不搜索<br/>直接 end]

    SKIP_FC --> SEND_COMPLETED
    SKIP_SEARCH --> SEND_COMPLETED

    ALL_WEB_SEARCH --> SEND_COMPLETED
    SEND_COMPLETED["发送<br/>response.completed"]

    SEND_COMPLETED --> SCANNER

    SCAN_ERR --> LOG_ERR["日志记录 stream read error"]
    LOG_ERR --> STREAM_END(["流结束"])
```

### 3.2 流式搜索子流程（finish_reason 后触发）

```mermaid
flowchart TB
    SUB_START(["allWebSearch=true<br/>进入搜索"]) --> EXTRACT_QUERY["从 tool_call arguments<br/>提取 searchQuery"]
    EXTRACT_QUERY --> CHECK_QUERY{"query 非空?"}
    CHECK_QUERY -->|否| FALLBACK["fallback 消息"]

    CHECK_QUERY -->|是| BACKEND_SWITCH{"SEARCH_BACKEND?"}

    BACKEND_SWITCH -->|searxng| SEARXNG_SS[handleSearXNGStreamingSearch]
    BACKEND_SWITCH -->|bing| BING_SS[Bing 流式搜索]

    SEARXNG_SS --> SEND_COMPLETED["发送 response.completed"]

    BING_SS --> RETRY_INIT["初始化重试<br/>attempt=0"]
    RETRY_INIT --> RETRY_LOOP{"attempt <=<br/>retryCount?"}

    RETRY_LOOP -->|否| ALL_EXHAUSTED["全部重试耗尽"]
    RETRY_LOOP -->|是| SEARCH["Searcher.Search"]
    SEARCH --> RESULT_CHECK{"有结果?"}
    RESULT_CHECK -->|否| INC_ATTEMPT["attempt++"]
    RESULT_CHECK -->|是| BUILD_REQ["BuildReInvokeRequest"]
    BUILD_REQ --> SEND_REQ["Proxy.Send (stream=true)"]
    SEND_REQ --> SCAN["逐行解析 SSE 流<br/>ParseChatStreamLine"]
    SCAN --> ACCUMULATE["累积 deltaText"]
    ACCUMULATE --> CHECK_FINISH{"finish_reason 到来?"}
    CHECK_FINISH -->|否| SCAN
    CHECK_FINISH -->|是| CHECK_ANSWER{"有效回答<br/>= !空 && !SEARCH_RESULT<br/>_INSUFFICIENT?"}

    CHECK_ANSWER -->|是| ACCEPT_ANSWER["接受结果"]
    CHECK_ANSWER -->|否| INC_ATTEMPT
    INC_ATTEMPT --> RETRY_LOOP

    ACCEPT_ANSWER --> SEND_RESULT["output_item.added<br/>output_text.delta/done<br/>output_item.done"]
    ALL_EXHAUSTED --> SEND_FALLBACK["output_item.added<br/>fallback 消息事件"]
    SEND_RESULT --> CONTINUE[继续 response.completed]
    SEND_FALLBACK --> CONTINUE

    FALLBACK --> CONTINUE
```

### 3.3 SearXNG 流式搜索子流程

```mermaid
flowchart TB
    START(["handleSearXNGStreamingSearch"]) --> SEARCH["Searcher.Search(ctx, query)<br/>调用 SearXNG JSON API"]
    SEARCH --> CHECK_RESULTS{"有结果?"}
    CHECK_RESULTS -->|否| TEXT_EMPTY["resultsText = ''"]
    CHECK_RESULTS -->|是| SUMM_CHECK{"SEARXNG_SUMMARIZE<br/>= true?"}

    SUMM_CHECK -->|否| FMT["formatSearchResults<br/>直接格式化"]
    SUMM_CHECK -->|是| MODEL_SUMM["BuildReInvokeRequest<br/>Proxy.Send (非流式)"]
    MODEL_SUMM --> EXTRACT["extractContentFromResponse<br/>提取总结文本"]
    EXTRACT --> FMT

    FMT --> ALLOC["NextOutputIndex()<br/>分配新 output_index"]
    TEXT_EMPTY --> ALLOC
    ALLOC --> SEND_ADDED["发送<br/>response.output_item.added"]
    SEND_ADDED --> SEND_DELTA["发送<br/>response.output_text.delta"]
    SEND_DELTA --> SEND_DONE["发送<br/>response.output_text.done"]
    SEND_DONE --> SEND_ITEM_DONE["发送<br/>response.output_item.done"]
    SEND_ITEM_DONE --> APPEND["追加到 output 列表"]
    APPEND --> END(["返回"])
```

---

## 四、工具调用处理流程图

```mermaid
flowchart TB
    START(["上游 SSE 含 tool_calls"]) --> PARSE["ParseChatStreamLine<br/>解析 toolCallDeltas"]

    PARSE --> LOOP["遍历每个 tool_call delta"]

    LOOP --> CHECK_INDEX{"已分配<br/>addedFCIndexes[index]?"}

    CHECK_INDEX -->|是| SKIP["跳过事件发送<br/>只记录参数"]
    CHECK_INDEX -->|否| ALLOC_INDEX["addedFCIndexes=true<br/>fcOutputIndex = NextOutputIndex()"]
    ALLOC_INDEX --> NAME_CHECK{"name 是什么?"}

    NAME_CHECK -->|web_search| IS_BUILTIN["fcIsBuiltin=true<br/>blockWebSearch 检查"]
    NAME_CHECK -->|其他| NOT_BUILTIN["fcIsBuiltin=false"]

    IS_BUILTIN --> SEND_BUILTIN["BuildStreamBuiltinToolItemAddedEvent<br/>type=web_search_call"]
    NOT_BUILTIN --> SEND_FC["BuildStreamFunctionCallItemAddedEvent<br/>type=function_call"]

    SEND_BUILTIN --> ARGS_CHECK{"有 arguments<br/>delta?"}
    SEND_FC --> ARGS_CHECK

    ARGS_CHECK -->|是+非内置| SEND_ARG_DELTA["发送<br/>function_call_arguments.delta"]
    ARGS_CHECK -->|否或内置| NEXT["下一个 delta"]

    SEND_ARG_DELTA --> NEXT

    NEXT --> MORE{"还有更多<br/>tool calls?"}
    MORE -->|是| LOOP
    MORE -->|否| END(["等待 finish_reason"])

    subgraph 聚合阶段[finish_reason 到来后]
        MERGE["mergeMap<br/>按 Index 合并所有参数"]
        MERGE --> LOOP_MERGE["遍历 mergeMap"]
        LOOP_MERGE --> CHECK_BUILTIN{"fcIsBuiltin?"}
        CHECK_BUILTIN -->|是| BUILD_SEARCH_ITEM["构建 web_search_call item<br/>含 action"]
        CHECK_BUILTIN -->|否| SEND_ARG_DONE["function_call_arguments.done"]
        SEND_ARG_DONE --> BUILD_FC_ITEM["构建 function_call item<br/>含 call_id/name/arguments"]
        BUILD_SEARCH_ITEM --> SEND_ITEM_DONE["output_item.done 发送"]
        BUILD_FC_ITEM --> SEND_ITEM_DONE
        SEND_ITEM_DONE --> MORE_MERGE{"还有更多?"}
        MORE_MERGE -->|是| LOOP_MERGE
        MORE_MERGE -->|否| AGG_DONE(["聚合完成"])
    end
```

---

## 五、路由选择流程图

```mermaid
flowchart TB
    START(["Proxy.selectBaseURL(path)"]) --> INIT["matchedURL = ''<br/>matchedLen = 0"]
    INIT --> LOOP["遍历 routeMap<br/>{prefix, url}"]

    LOOP --> PREFIX_CHECK{"strings.HasPrefix(path, prefix)<br/>且 len(prefix) > matchedLen?"}
    PREFIX_CHECK -->|是| UPDATE["matchedURL = url<br/>matchedLen = len(prefix)"]
    PREFIX_CHECK -->|否| SKIP_ENTRY[跳过此条目]

    UPDATE --> MORE{"还有更多<br/>条目?"}
    SKIP_ENTRY --> MORE

    MORE -->|是| LOOP
    MORE -->|否| FINAL_CHECK{"matchedURL 为空?"}

    FINAL_CHECK -->|是| USE_DEFAULT["返回 defaultBaseURL"]
    FINAL_CHECK -->|否| USE_MATCHED["返回 matchedURL"]

    USE_DEFAULT --> BUILD_URL["+ /v1/chat/completions"]
    USE_MATCHED --> BUILD_URL
    BUILD_URL --> RETURN(["最终上游 URL"])
```

---

## 六、输入格式转换流程图

```mermaid
flowchart TB
    START(["convertInput(raw)"]) --> EMPTY_CHECK{"raw 为空?"}
    EMPTY_CHECK -->|是| EMPTY_RET["返回 nil"]

    EMPTY_CHECK -->|否| STRING_CHECK{"raw[0] == '\"'<br/>字符串?"}
    STRING_CHECK -->|是| STRING_CONV["解析为 string<br/>→ [{role:user, content:string}]"]
    STRING_CHECK -->|否| ARRAY_CHECK{"raw[0] == '['<br/>数组?"}

    ARRAY_CHECK -->|否| ERROR["返回 error<br/>input must be string or array"]

    ARRAY_CHECK -->|是| PARSE["json.Unmarshal → []inputItem"]
    PARSE --> LOOP["遍历 items"]

    LOOP --> TYPE_CHECK{"item.type?"}

    TYPE_CHECK -->|function_call_output| FCO["output 非空?"]
    FCO -->|是| USER_MSG["→ {role:user, content: '工具执行结果:\\n' + output}"]
    FCO -->|否| SKIP["跳过此 item"]

    TYPE_CHECK -->|web_search_call| WSC["output 非空?"]
    WSC -->|是| WEB_MSG["→ {role:user, content: 'Web search results:\\n' + output}"]
    WSC -->|否| SKIP

    TYPE_CHECK -->|message 或无 type| MESSAGE["按 role 处理"]
    MESSAGE --> ROLE_CHECK{"item.role?"}
    ROLE_CHECK -->|developer| TO_SYSTEM["→ role=system"]
    ROLE_CHECK -->|user| TO_USER["→ role=user"]
    ROLE_CHECK -->|assistant| TO_ASSISTANT["→ role=assistant"]
    ROLE_CHECK -->|其他| SKIP

    TO_SYSTEM --> CONTENT["extractContent(item.Content)"]
    TO_USER --> CONTENT
    TO_ASSISTANT --> CONTENT
    CONTENT --> APPEND["追加到 messages"]

    TYPE_CHECK -->|其他类型| SKIP

    SKIP --> MORE{"还有<br/>更多?"}
    USER_MSG --> MORE
    WEB_MSG --> MORE
    APPEND --> MORE

    MORE -->|是| LOOP
    MORE -->|否| RETURN(["返回 []Message"])
    STRING_CONV --> RETURN
    EMPTY_RET --> RETURN
    ERROR --> RETURN
```

---

## 七、搜索完整流程图（含所有分支）

```mermaid
flowchart TB
    TRIGGER(["检测到 web_search tool_call"])

    TRIGGER --> BLOCK{"BLOCK_WEB_SEARCH?"}

    BLOCK -->|true| TOOLS_NIL["tools 设为 nil<br/>web_search 不传给上游"]
    BLOCK -->|false| TOOLS_PASS["web_search 映射为 function<br/>传入上游"]

    TOOLS_NIL --> WAIT_CLIENT["等待客户端自行处理搜索"]
    TOOLS_PASS --> WAIT_RESP["等待上游响应"]

    WAIT_RESP --> CHECK_RESP{"上游返回<br/>tool_calls?"}

    CHECK_RESP -->|否| DIRECT_CONV["直接转换响应<br/>无搜索参与"]
    CHECK_RESP -->|是工具调用<br/>非 web_search| DIRECT_CONV
    CHECK_RESP -->|是 web_search| SEARCH_EXEC["执行搜索"]

    SEARCH_EXEC --> BACKEND{"后端类型"}

    BACKEND -->|searxng| SEARXNG_BRANCH
    BACKEND -->|bing| BING_BRANCH

    subgraph SEARXNG_BRANCH[SearXNG 路径]
        S1["调用 SearXNG JSON API"]
        S2["解析 results"]
        S3_CHECK{"SEARXNG_SUMMARIZE?"}
        S3["直接格式化文本"]
        S4["模型总结"]
        S5["构建 web_search_call + message<br/>json 响应"]
        S1 --> S2 --> S3_CHECK
        S3_CHECK -->|false| S3 --> S5
        S3_CHECK -->|true| S4 --> S5
    end

    subgraph BING_BRANCH[Bing 路径]
        B1["搜索 Bing SERP HTML"]
        B2["goquery 解析提取结果"]
        B3["并发抓取页面内容"]
        B4["过滤无效结果"]
        B5["重试循环<br/>搜索+模型总结"]
        B6["成功? → 接受结果<br/>失败? → fallback"]
        B7["构建含文本的 json 响应"]

        B1 --> B2 --> B3 --> B4 --> B5 --> B6 --> B7
    end

    SEARXNG_BRANCH --> RESP["返回 Responses API<br/>格式响应"]
    BING_BRANCH --> RESP
    DIRECT_CONV --> RESP
    WAIT_CLIENT --> RESP
```

---

## 八、错误处理流程图

```mermaid
flowchart TB
    ERROR_CHECK{"请求哪个环节出错?"}

    ERROR_CHECK -->|"读取请求体<br/>失败"| E400["HTTP 400<br/>{'error':'invalid request body'}"]
    ERROR_CHECK -->|"请求转换<br/>失败"| E500["HTTP 500<br/>{'error':'conversion failed'}<br/>输入非 string/array"]
    ERROR_CHECK -->|"上游请求<br/>网络错误"| E502["HTTP 502<br/>{'error':'upstream request failed'}<br/>DNS/超时/连接拒绝"]
    ERROR_CHECK -->|"上游返回<br/>非 200"| PROXY_ERROR["HTTP 与上游一致<br/>透传上游错误体"]
    ERROR_CHECK -->|"响应转换<br/>失败"| E500_2["HTTP 500<br/>{'error':'response conversion failed'}<br/>仅非流式"]
    ERROR_CHECK -->|"流式读取<br/>异常"| LOG_ONLY["不返回 HTTP 错误<br/>仅记录日志<br/>stream read error"]
    ERROR_CHECK -->|"搜索全部<br/>重试失败"| FALLBACK["HTTP 200<br/>fallback 文本<br/>'当前未搜索到相关信息'"]
```
