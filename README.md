# Clash Web

Clash Web 是面向无桌面 Linux 服务器的 mihomo Web 控制台。它采用独立实现，不复用 Clash Verge Rev 的源代码或品牌资源，但保留相近的信息结构：首页、代理、订阅、连接、规则、日志、测试和设置。

## 架构

```text
Browser ──HTTPS──> Nginx/Caddy ──HTTP──> clash-web serve (unprivileged)
                                             │
                    ┌────────────────────────┼─────────────────────┐
                    │ REST/WebSocket         │ restricted IPC      │
                    ▼                        ▼                     │
             mihomo Unix socket       clash-web helper (root)     │
                    ▲                        │                     │
                    └────────── mihomo process + TUN ─────────────┘
```

Web 服务不以 root 运行，也不会将 mihomo Controller 暴露到 TCP 网络。只有 helper 能启动内核、验证并原子切换配置、管理 TUN/路由能力，以及执行受限的官方内核和 GeoData 更新。

## 本地开发

需要 Go 1.24、Node.js 24 和 pnpm 10。

```bash
cd ui
pnpm install
pnpm build
cd ..
go test ./...
go run ./cmd/clash-web serve --config ./dev-config.yaml
```

Windows 开发配置示例：

```yaml
listen: 127.0.0.1:8080
data_dir: data
runtime_dir: data/run
mihomo_binary: mihomo.exe
mihomo_controller: http://127.0.0.1:9090
helper_socket: tcp://127.0.0.1:9088
```

首次启动会在数据目录生成 `bootstrap-password`。用户名固定为 `admin`，首次登录后应立即修改密码。

## 构建 deb

发布脚本会构建 amd64 和 arm64 两种包，并从固定 tag 构建 mihomo：

- mihomo tag：`v1.19.27`
- commit：`5184081ac327394d9e15fa5d5f9f4a61e723fd94`

脚本在 tag 与 commit 不一致时终止，产物的 SHA-256 会写入包内的 `BUILD-INFO`。

```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
VERSION=0.1.0 make release
sudo apt install ./dist/amd64/clash-web_0.1.0_amd64.deb
sudo cat /var/lib/clash-web/bootstrap-password
```

安装后访问 `http://服务器地址:8080`。公网环境必须先配置 HTTPS。

## HTTPS 反向代理

Caddy：

```caddyfile
clash.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

使用反向代理时，将 `/etc/clash-web/config.yaml` 中的 `listen` 改为 `127.0.0.1:8080`，把 `trusted_proxy` 改为 `true`，然后执行：

```bash
sudo systemctl restart clash-web
```

## Docker

GitHub Actions 会将多架构镜像发布到：

```text
ghcr.io/lazy-warfarin/clash-web
```

只使用普通代理端口时，可以直接运行：

```bash
docker run -d \
  --name clash-web \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 7890:7890/tcp \
  -p 7890:7890/udp \
  -v clash-web-data:/var/lib/clash-web \
  ghcr.io/lazy-warfarin/clash-web:latest

docker exec clash-web cat /var/lib/clash-web/bootstrap-password
```

如果需要让容器管理服务器的 TUN 和主机路由，使用仓库中的 `compose.yaml`。该配置使用 host 网络，并显式授予 `NET_ADMIN`、`NET_RAW`、`NET_BIND_SERVICE` 和 `/dev/net/tun`；这些权限只应在可信服务器上启用：

```bash
docker compose up -d
docker compose exec clash-web cat /var/lib/clash-web/bootstrap-password
```

容器入口仍运行两个独立进程：helper 保留必要的 root 网络权限，Web 服务通过 `gosu` 以 UID/GID `10001` 运行。

## 自动发布

`.github/workflows/release.yml` 提供三种流程：

- Pull Request：只执行 Go 测试、前端类型检查与构建检查。
- `main` 分支：生成 amd64/arm64 deb Artifact，并推送 `main`、`sha-*` 和 `latest` Docker 标签。
- `v*` 标签：创建 GitHub Release、附加两个 deb，并推送版本号、主次版本号、tag、SHA 和 `latest` Docker 标签。

也可以在 Actions 页面手动运行并填写版本号。Docker 镜像使用 Buildx 构建 `linux/amd64` 与 `linux/arm64`，同时发布 SBOM 和 provenance attestation。

## 运维

```bash
sudo systemctl status clash-web clash-web-helper
sudo journalctl -u clash-web -u clash-web-helper -f
sudo clash-web reset-password
```

配置和订阅保存在 `/var/lib/clash-web`，普通卸载不会删除这些数据。

设置页中的局域网、IPv6、端口、DNS 和 TUN 设置会保存到 `/var/lib/clash-web/runtime/overrides.yaml`，以后更新或重新启用订阅时仍会覆盖到最终运行配置。高级设置里的“当前配置”用于直接查看和验证最终 YAML。

## 安全约束

- 订阅下载仅接受 HTTP/HTTPS，默认阻止回环、私网、链路本地和保留地址。
- 所有浏览器 API 与 WebSocket 都要求登录；写操作执行 Origin 检查。
- helper IPC 不接受任意命令、任意 URL 或任意文件路径，只提供预定义的状态、启停、配置、内核和 GeoData 操作。
- 在线内核更新只查询 MetaCubeX/mihomo 的 GitHub 官方稳定版，只接受当前 Linux 架构的标准资产，并核对 GitHub 发布的 SHA-256；内置内核不会被覆盖，可随时切回。
- GeoData 更新只接受 MetaCubeX/meta-rules-dat 官方发布中的预定义资产，并在原子替换前核对 SHA-256。

## 许可证

Clash Web 使用 [Apache License 2.0](./LICENSE)。随 deb 独立分发的 mihomo 使用 GPL-3.0，并在包中附带其许可证、源码 commit 和构建校验信息。
