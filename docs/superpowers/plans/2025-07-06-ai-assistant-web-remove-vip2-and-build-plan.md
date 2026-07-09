# ai-assistant-web 移除 VIP2 限制并打包 Windows 安装包

## 目标

1. 下线 `ai-assistant-web` 的 VIP2 限制，使其免费开放使用
2. 将桌面应用整体打包成 Windows 安装包（`.exe` installer）

---

## 当前状态

| 项目 | 状态 | 备注 |
|---|---|---|
| VIP 限制代码修改 | ✅ 已完成 | `ai-assistant-web/server.go` |
| `go.mod` 版本修正 | ✅ 已完成 | `go 1.26.0` → `go 1.25` |
| Go 升级到 1.25 | ✅ 已完成 | 本地 `go version go1.25.0 windows/amd64` |
| NSIS 安装 | ✅ 已完成 | `C:\Program Files (x86)\NSIS` |
| Wails CLI 安装 | ❌ 待解决 | `go install` 因 GitHub 认证失败 |
| 前端构建 | ⏳ 待执行 | 需要 Wails CLI |
| Windows 安装包 | ⏳ 待执行 | 需要 Wails CLI |

---

## 已完成的代码改动

### 1. `ai-assistant-web/server.go`

- `/api/vip-status` 接口固定返回 `ok: true`
- 移除 `getAIConfigs` 中的 `requireVip2` 调用
- 移除 `getPrompts` 中的 `requireVip2` 调用
- 移除 `session` 中的 `requireVip2` 调用
- 移除 `summaryChatStream` 中的 `requireVip2` 调用
- 移除 `shareText` 中的 `requireVip2` 调用
- 保留 `requireVip2` 和 `vipDeniedMessage` 函数，便于后续恢复 VIP 限制

### 2. `go.mod`

- `go 1.26.0` → `go 1.25`

---

## 待执行步骤

### 步骤 1：安装 Wails CLI

由于 `go install github.com/wailsio/wails/v2/cmd/wails@latest` 因 GitHub 认证失败，可选方案：

#### 方案 A：手动下载预编译版（推荐）

1. 浏览器打开：https://github.com/wailsio/wails/releases/latest
2. 下载 `wails_*_windows_amd64.zip`
3. 解压到 `C:\tools\wails\`
4. 将 `C:\tools\wails\` 加入系统 PATH
5. 验证：`wails doctor`

#### 方案 B：配置 GitHub 认证

```powershell
git credential-manager github login
go install github.com/wailsio/wails/v2/cmd/wails@latest
```

### 步骤 2：验证构建环境

```powershell
go version
wails doctor
makensis -VERSION
```

### 步骤 3：构建 Windows 安装包

在仓库根目录执行：

```powershell
wails build --clean --platform windows/amd64 --nsis
```

构建成功后输出位置：

```
build/bin/go-stock-windows-amd64-installer.exe
```

### 步骤 4：验证安装包

1. 双击运行安装包
2. 安装后启动 go-stock
3. 打开浏览器访问 http://localhost:18888
4. 确认 AI 助手 Web 版无需 VIP 即可使用

---

## 恢复 VIP 限制的方法

1. 恢复 `ai-assistant-web/server.go` 中 `vipStatus` 函数原逻辑
2. 在 `getAIConfigs`、`getPrompts`、`session`、`summaryChatStream`、`shareText` 中重新加上 `if !requireVip2(w) { return }`

---

## 风险与注意事项

- 开放后，任何能访问 `ai-assistant-web` 服务的用户都可以使用 AI 助手
- 桌面应用启动时会自动启动 `ai-assistant-web` 服务
- 如果直接暴露到公网，建议增加额外的访问控制
