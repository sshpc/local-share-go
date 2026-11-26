# local-share-go

一个简单的手机－电脑互传工具：浏览器上传，生成下载链接和二维码，手机扫码直接下载。单文件 Go 程序，零前端依赖，编译出来一个可执行文件即可运行。

## 功能

- 网页上传文件（点击或拖拽）
- 上传成功后返回下载链接与二维码，手机扫码即可下载
- 基于 UUID 的文件 ID，下载地址不含原始路径
- 支持断点续传（`http.ServeFile` 提供的 `Range` 请求）
- 配置支持配置文件 + 环境变量覆盖

## 运行

需要 Go 1.25 或更高版本。

```bash
go run .
```

或先编译再运行：

```bash
go build -o local-share-go .
./local-share-go
```

启动后访问 `http://localhost:8085`。

注意：仓库内的 `config.json` 把端口配置为 8081，且 `public_base_url` 仍是 `http://localhost:8085`。直接运行本仓库时实际监听 8081，但生成的下载链接指向 8085（无服务），需要先按下方「配置」一节把两者改成一致。

不加载配置文件时默认监听 `0.0.0.0:8085`，上传目录为 `./uploads`，启动时自动创建。

### 交叉编译

```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o upload-tool-windows-amd64.exe .

# Linux
GOOS=linux GOARCH=amd64 go build -o upload-tool-linux-amd64 .
```

## 配置

支持 `config.json` 和环境变量，环境变量优先级更高。程序从**当前工作目录**读取 `config.json`。

```json
{
  "server_addr": "0.0.0.0:8081",
  "upload_dir": "./uploads",
  "public_base_url": "http://localhost:8085"
}
```

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `server_addr` | `SERVER_ADDR` | `0.0.0.0:8085` | 监听地址与端口 |
| `upload_dir` | `UPLOAD_DIR` | `./uploads` | 文件存储目录 |
| `public_base_url` | `PUBLIC_BASE_URL` | `http://localhost:8085` | 生成下载链接和二维码用的外部地址 |

> **`public_base_url` 必须与实际访问地址一致。** 下载链接和二维码都基于它拼接，写错时页面显示的链接和二维码会指向打不开的地址。局域网内给手机扫码使用时，要填本机的局域网 IP，例如 `http://192.168.1.10:8081`，不能用 `localhost`。

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/` | 上传页面 |
| `POST` | `/upload` | 接收 `multipart/form-data`，字段名 `file`；成功返回结果页，失败返回纯文本错误 |
| `GET` | `/download/{id}` | 按文件 ID 下载，响应为 `attachment` |
| `GET` | `/qr/{id}` | 返回该文件的下载二维码 PNG（256×256） |

## 存储布局

上传的文件以 `UUID + 原始扩展名` 命名存放：

```
uploads/
└── 3f2b1c8a-9d4e-4a71-b0c3-5e6f7a8b9c0d.pdf
```

原始文件名不保存，下载时浏览器得到的是 `UUID.扩展名`。文件不会自动清理，删除请手动处理 `upload_dir` 下的内容。

## 已知限制

当前版本定位是局域网内自用，投放到公网前需要注意：

- **上传大小限制不严格**：`ParseMultipartForm` 的参数只是内存缓冲阈值，大小检查发生在整个请求体接收完之后，超大文件仍会被完整收下才返回 400。
- **下载按前缀匹配**：下载先按常见扩展名逐个查找，找不到再遍历上传目录按文件名前缀匹配，并非精确 ID 查询；文件数量多时效率下降，且过短的 ID 前缀可能命中错误的文件。
- **无鉴权、无有效期**：任何人拿到链接即可下载，文件长期保留。
- **未注册路径落到上传页**：根路径注册在 `/`，任意未匹配的路径都会返回上传页而不是 404。
- **无请求日志与超时设置**：直接使用 `http.ListenAndServe` 和默认多路复用器，未设置读写超时。
- **启动失败退出码为 0**：端口被占用等监听错误只打印一行日志，不会以非零状态退出。

## 依赖

- [github.com/google/uuid](https://github.com/google/uuid) — 生成文件 ID
- [github.com/skip2/go-qrcode](https://github.com/skip2/go-qrcode) — 生成二维码

## 许可证

[MIT](LICENSE)
