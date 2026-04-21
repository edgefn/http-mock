<div align="center">

# http-mock

文件驱动的轻量 HTTP Mock Server。

[![CI](https://github.com/edgefn/http-mock/actions/workflows/ci.yml/badge.svg)](https://github.com/edgefn/http-mock/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/edgefn/http-mock)](https://github.com/edgefn/http-mock/releases)
[![Docker Image](https://img.shields.io/badge/ghcr.io-edgefn%2Fhttp--mock-blue)](https://github.com/edgefn/http-mock/pkgs/container/http-mock)
[![License](https://img.shields.io/github/license/edgefn/http-mock)](https://github.com/edgefn/http-mock/blob/main/LICENSE)

</div>

`http-mock` 用一个 `routes.yaml` 把请求映射到本地响应文件，适合在本地开发、集成测试、代理联调和离线回放中稳定复现上游 HTTP 响应。

它不会引入复杂的脚本 DSL。对外只保留两层概念：

- `routes.yaml`：声明路径、方法、可选匹配规则和响应文件
- 数据根目录：存放 JSON、SSE、音频等响应文件，可按 endpoint 分目录组织

## 特性

- 文件驱动：响应内容来自数据根目录下的静态文件。
- 路由简单：支持精确路径和 `{param}` path segment 模板。
- 请求匹配：支持按请求 header 或 JSONPath 选择不同 mock 响应。
- 内容类型推断：内置 `.json`、`.sse`、`.mp3` 的常见 content type。
- 配置校验：启动前检查 `routes.yaml` 和响应文件是否存在。
- 容器友好：提供 Dockerfile，可直接构建镜像运行。

## 安装

使用 Go 安装：

```bash
go install github.com/edgefn/http-mock/cmd/http-mock@latest
```

使用 Docker：

```bash
docker run --rm -p 18080:18080 \
  -v "$PWD/http-mock-data:/data:ro" \
  ghcr.io/edgefn/http-mock:latest
```

从源码运行：

```bash
git clone https://github.com/edgefn/http-mock.git
cd http-mock
go test ./...
go build ./cmd/http-mock
```

## 快速开始

准备数据目录：

```text
http-mock-data/
├── routes.yaml
└── v1/
    └── chat/
        └── completions/
            └── mock.json
```

编写 `routes.yaml`：

```yaml
routes:
  - path: /v1/chat/completions
    method: POST
    response_file: v1/chat/completions/mock.json
    content_type: application/json
```

启动服务：

```bash
http-mock serve --routes routes.yaml --data-root ./http-mock-data --listen :18080
```

发送请求：

```bash
curl -i http://127.0.0.1:18080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

## 命令

启动服务：

```bash
http-mock serve \
  --routes routes.yaml \
  --data-root ./http-mock-data \
  --listen :18080
```

校验配置：

```bash
http-mock validate \
  --routes routes.yaml \
  --data-root ./http-mock-data
```

常用 Make 命令：

```bash
make validate ROUTES=routes.yaml DATA_ROOT=../http-mock-data
make run ROUTES=routes.yaml DATA_ROOT=../http-mock-data LISTEN=:18080
make test
make build
```

## 路由规则

`routes.yaml` 顶层字段是 `routes`，每个路由支持：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `path` | 是 | 请求路径，必须以 `/` 开头 |
| `method` | 否 | HTTP 方法，默认 `GET`，会统一转为大写 |
| `response_file` | 是 | 响应文件路径；相对路径基于 `--data-root` 解析 |
| `content_type` | 否 | 响应 `Content-Type`，为空时根据扩展名推断 |
| `status_code` | 否 | 响应状态码，默认 `200` |
| `match` | 否 | 额外匹配条件，支持 header、query 或 JSONPath |

服务会在请求进入时检查 `routes.yaml` 的修改时间；文件变更后会懒加载新路由，加载失败时继续沿用上一份可用配置并记录日志。`response_file` 内容每次请求都会重新读取。

路径支持精确匹配：

```yaml
routes:
  - path: /v1/responses
    method: POST
    response_file: v1/responses/mock.json
```

也支持 `{param}` 模板。模板参数匹配同一 path segment 内的非空内容，适合 Gemini native endpoint 这类路径：

```yaml
routes:
  - path: /v1beta/models/{model}:generateContent
    method: POST
    response_file: v1beta/models/{model}:generateContent/text_mock.json
    content_type: application/json
```

## 请求匹配

同一路径和方法下可以声明多条 route，`http-mock` 会按配置顺序寻找第一条匹配项。

按 JSONPath 匹配请求体：

```yaml
routes:
  - path: /v1/chat/completions
    method: POST
    match:
      json_path: stream
      equals: "true"
    response_file: v1/chat/completions/stream.sse
    content_type: text/event-stream

  - path: /v1/chat/completions
    method: POST
    response_file: v1/chat/completions/mock.json
    content_type: application/json
```

按 header 匹配：

```yaml
routes:
  - path: /v1/responses
    method: POST
    match:
      header: X-Mock-Case
      equals: error
    response_file: v1/responses/error.json
    status_code: 500
```

按 query 匹配，适合 Gemini native 流式接口这类 `?alt=sse` 请求。`path` 只写 URL path，不写 query：

```yaml
routes:
  - path: /v1beta/models/{model}:streamGenerateContent
    method: POST
    match:
      query: alt
      equals: sse
    response_file: v1beta/models/{model}:streamGenerateContent/text_real.sse
    content_type: text/event-stream
```

## 响应文件

`response_file` 可以指向任意本地文件。常见扩展名会自动推断 content type：

| 扩展名 | Content-Type |
| --- | --- |
| `.json` | `application/json` |
| `.sse` | `text/event-stream` |
| `.mp3` | `audio/mpeg` |

其他扩展名默认使用 `application/octet-stream`。如果需要固定 content type，请在 route 中显式设置 `content_type`。

## 错误码

- `404 mock route not found`：没有任何路径匹配。
- `404 mock route not matched`：路径和方法匹配，但 `match` 条件未命中。
- `405 method not allowed`：路径存在，但 HTTP 方法不匹配。
- `500`：响应文件读取失败或服务内部错误。

## GitHub Actions

- push 到 `main`：运行测试、编译检查，并发布 `ghcr.io/edgefn/http-mock:edge` 和 `sha-*` 镜像。
- push `v*` tag：发布 GitHub Release 二进制产物，并发布 `latest`、semver 和 tag 镜像。

## License

[MIT](./LICENSE)
