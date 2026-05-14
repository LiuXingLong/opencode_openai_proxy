# opencode-openai-proxy

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![GitHub](https://img.shields.io/badge/GitHub-LiuXingLong-181717?logo=github)](https://github.com/LiuXingLong/opencode-openai-proxy)

OpenAI Responses API 代理 — 将 `/v1/responses` 请求转换为 `/v1/chat/completions` 请求并转发到上游服务。

## 架构

```
客户端 (@ai-sdk/openai)         代理 (Go)                    上游服务 (@ai-sdk/openai-compatible)
       │                          │                                │
       │  POST /v1/responses      │                                │
       │─────────────────────────►│                                │
       │                          │  POST /v1/chat/completions     │
       │                          │───────────────────────────────►│
       │                          │                                │
       │  Responses API 格式      │  Chat Completions 格式          │
       │◄─────────────────────────│◄───────────────────────────────│
```

## 快速开始

```bash
# 编译
./manage.sh build

# 启动（后台运行）
./manage.sh start

# 重启
./manage.sh restart

# 停止
./manage.sh stop

# 重新打开日志文件（日志轮转后使用）
./manage.sh reopen
```

### Docker

```bash
# 构建并启动
docker compose up -d

# 查看日志
docker compose logs -f

# 停止
docker compose down

# 使用自定义环境变量
UPSTREAM_BASE_URL=https://your-upstream.com/zen docker compose up -d
```

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `UPSTREAM_BASE_URL` | `https://opencode.ai/zen` | 上游 Chat Completions 服务地址 |
| `LISTEN_ADDR` | `:8082` | 代理监听地址 |
| `LOG_FILE` | `./logs/proxy.log` | 日志文件路径 |

## API 使用

未传入 `Authorization` header 或 app_key 为空时，默认使用 `Bearer public`。

### 非流式

```bash
curl -X POST http://localhost:8082/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "big-pickle",
    "input": "你好",
    "stream": false
  }'
```

### 流式

```bash
curl -N -X POST http://localhost:8082/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "big-pickle",
    "input": "你好",
    "stream": true
  }'
```

### 多轮对话

```bash
curl -X POST http://localhost:8082/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <app_key>" \
  -d '{
    "model": "big-pickle",
    "input": [
      {"role": "user", "content": "你好"},
      {"role": "assistant", "content": "你好！有什么可以帮助你的吗？"},
      {"role": "user", "content": "今天天气怎么样？"}
    ],
    "instructions": "你是一个友好的助手",
    "stream": false
  }'
```

### 响应格式

```json
{
  "id": "resp_abc123",
  "object": "response",
  "created_at": 1778753686,
  "status": "completed",
  "model": "deepseek-v4-flash",
  "output": [
    {
      "id": "msg_abc123",
      "type": "message",
      "role": "assistant",
      "content": [
        {
          "type": "output_text",
          "text": "你好！有什么可以帮助你的吗？",
          "annotations": []
        }
      ]
    }
  ],
  "usage": {
    "input_tokens": 84,
    "output_tokens": 159,
    "total_tokens": 243,
    "output_tokens_details": {
      "reasoning_tokens": 0
    }
  }
}
```

## 请求转换说明

| Responses API | Chat Completions API |
|---|---|
| `input` (string) | → `messages: [{role:"user", content: <input>}]` |
| `input` (array) | → `messages`（过滤 message 类型 item，role 映射） |
| `instructions` | → `messages` 头部插入 system message |
| `max_output_tokens` | → `max_tokens` |
| `text.format` | → `response_format` |

## 日志

日志同时输出到控制台和文件，JSON 格式，每条日志包含 `trace_id` 用于追踪请求全链路。

```json
{"time":"...","level":"INFO","msg":"incoming request","trace_id":"abc-123","method":"POST","path":"/v1/responses","body":"..."}
{"time":"...","level":"INFO","msg":"upstream request","trace_id":"abc-123","method":"POST","url":"...","body":"..."}
{"time":"...","level":"INFO","msg":"upstream response","trace_id":"abc-123","status":200,"duration":"1.5s"}
{"time":"...","level":"INFO","msg":"outgoing response","trace_id":"abc-123","status":200,"duration":"1.5s","body":"..."}
```

- 删除日志文件或 `logs/` 目录后，下次写入会自动重建
- 日志轮转后执行 `kill -HUP <pid>` 或 `./manage.sh reopen` 重新打开日志文件

## 项目结构

```
opencode_openai_proxy/
├── main.go              # 入口
├── manage.sh            # 管理脚本（build/start/stop/restart/reopen）
├── Dockerfile           # Docker 镜像构建
├── docker-compose.yml   # Docker Compose 编排
├── config/config.go     # 配置
├── logger/logger.go     # 日志（slog JSON + 文件，自动重建）
├── middleware/
│   ├── trace.go         # traceID
│   └── auth.go          # 认证（默认 Bearer public）
├── converter/
│   ├── request.go       # 请求转换
│   └── response.go      # 响应转换（非流式 + 流式）
├── proxy/proxy.go       # 上游转发
└── handler/
    ├── responses.go     # /v1/responses 端点
    └── health.go        # /health 端点
```
