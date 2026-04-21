# http-mock

文件驱动的轻量 HTTP Mock Server。

对外只保留两层概念：

- `routes.yaml`：声明路径、方法、可选匹配规则、返回文件
- 数据根目录下的响应文件：可按 endpoint 组织，例如 `v1/chat/completions/multimodal_real.json`

`routes.yaml` 的 `path` 支持精确路径，也支持 `{param}` 模板。模板参数匹配同一 path segment 内的非空内容，适合 `/v1beta/models/{model}:generateContent` 这类 Gemini native endpoint。

常用命令：

```bash
make validate ROUTES=routes.yaml DATA_ROOT=../http-mock-data
make run ROUTES=routes.yaml DATA_ROOT=../http-mock-data LISTEN=:18080
```
