# 节点体检 · ipscan

一个纯 Go 原生桌面的代理节点质量检查器：粘贴 Clash / Xray / sing-box 订阅链接，
解析全部节点，逐条测量 TCP 握手延迟、抖动、丢包与 TLS 握手/证书，并查询 IP 归属、
运营商与机房/代理标记。界面用 [Onevra](https://github.com/oneo-me/onevra)（MyGo 原生 UI
设计系统）在 GPU 上绘制：暖白纸张底色、陶土强调色、卡片式统计与可排序/拖拽列宽的数据表，
不依赖 WebView。

## 构建与运行

```sh
go build -o ipscan.exe ./cmd/ipscan
./ipscan.exe
```

- Go 1.27+，Windows / macOS / Linux。
- `third_party/yaml` 是 yaml.v3 的本地副本（MIT/Apache），让没有模块代理的环境也能离线构建。

## 支持的订阅格式

| 格式 | 识别方式 |
| --- | --- |
| 节点链接列表 | 每行一个 `vmess://` `vless://` `trojan://` `ss://` `ssr://` `hysteria2://` `tuic://` `socks5://` |
| base64 订阅 | 自动解码（标准 / URL-safe，带或不带 padding），可嵌套检测内部格式 |
| Clash / Mihomo YAML | `proxies[].type` 覆盖 vmess、vless、trojan、ss、ssr、hysteria、hysteria2、tuic、socks5、http |
| Xray / v2ray JSON | `outbounds[].protocol` + `streamSettings`，读取 ws/grpc/h2/httpupgrade/xhttp 与 TLS/REALITY |
| sing-box JSON | `outbounds[]`，读取 `tls`、`reality`、`utls`、`transport`、`obfs` |

单条坏节点只会记入“跳过 N 条”，不影响其余节点。

## 展示内容

- 节点表：节点（协议标签）、地址、端口（TCP/UDP）、IP、地区/运营商、传输协议（含路径/服务名/流控）、
  TLS（含 SNI）、延迟、抖动、丢包、评分；点击行在右侧查看全部参数。
- IP 质量：解析出的 IPv4/IPv6、DNS 耗时、位置、运营商、ASN、机房/移动网络、代理标记。
- 传输质量：TCP 握手 min/avg/max、抖动、丢包、评分（优秀/良好/一般/较差/不可达）。
- TLS：版本、套件、证书 CN/签发者/到期、主机名校验；QUIC 类节点（hysteria2/tuic）
  正常不监听 TCP，标为“未探测”并保留 IP 质量结果。

IP 归属查询先走 ip-api.com 批量接口，被墙或限流时自动回退到 HTTPS 的 ipwho.is；
两者都失败时状态栏显示“归属查询失败”，延迟/TLS 结果不受影响。只发送解析出的公网 IP。

## 界面

- Onevra 主题（明/暗自动跟随系统）：`theme.Apply` + 暖白/暖炭底色与陶土强调色。
- 组件：`Button`、`TextBox`、`Badge`、`Tabs`、`Table/TableRow`、`ScrollView`、`Progress`、
  `Spinner`、`Icon`（Lucide）。
- 列表页：订阅卡片、4 张统计卡、筛选/搜索、可拖拽调整列宽的数据表；窗口宽度 < 1240 时
  自动收紧列宽，避免右侧列被裁掉。
- 详情页：整页展示全球探测、连接质量、TLS 证书、IP 归属与全部节点参数，返回列表一键切换。

## 全球多点检测

点击节点行打开详情，在“全球网络测试”里执行 **全球 Ping**：通过
[Globalping](https://globalping.io) 的公开 API 从 8 个海外探测点（国家、城市、运营商、ASN）
对节点地址做 ICMP ping，返回每个探测点的平均延迟与丢包，并汇总可达数/中位延迟/平均丢包。
结果同时写入表格“延迟”列的第二行（`全球 xxms`），悬停可看汇总。

- 每个节点按需触发，同一时间只跑一个测量；重新加载订阅会取消并清空。
- 未认证额度约 500 次测量/小时；目标地址会发送给 `api.globalping.io`。
- ping.pe 的前端使用私有 WebSocket 协议、没有公开 API，因此未直接接入；`remote.Pinger`
  是接口，后续可替换或增加其他探测服务。

```sh
GOPROXY=off go test ./...
```


## 代码结构

```
cmd/ipscan            窗口与依赖装配
internal/model        节点模型（协议/传输/安全参数）
internal/subscription 下载与格式识别、URI/Clash/Xray/sing-box 解析
internal/probe        DNS、TCP、TLS 探测，评分与 IP 归属
internal/remote       第三方多地点探测（Globalping）
internal/scan         有限并发调度与进度上报
internal/app          应用状态、异步任务与取消（不依赖 UI）
internal/screen       仅绘图：订阅栏、统计、节点表、详情抽屉
```

## 测试

```sh
GOPROXY=off go test ./...
```

解析、探测（本地回环监听器）、并发调度与界面渲染均有测试。
