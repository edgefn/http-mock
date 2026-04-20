# http-mock

文件驱动的轻量 HTTP Mock Server。

对外只保留两层概念：

- `routes.yaml`：声明路径、方法、可选匹配规则、返回文件
- `responses/`：真实返回内容，像 ONR 的 `testdata`

常用命令：

```bash
make validate ROUTES=routes.yaml DATA_ROOT=../http-mock-data
make run ROUTES=routes.yaml DATA_ROOT=../http-mock-data LISTEN=:18080
```
