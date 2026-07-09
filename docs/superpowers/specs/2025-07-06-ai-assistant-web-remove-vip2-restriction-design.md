# ai-assistant-web 移除 VIP2 限制设计文档

## 背景

`ai-assistant-web` 是 go-stock 项目下的独立 Web AI 助手服务，目前仅对 VIP2 及以上有效赞助用户开放。后端通过 `/api/vip-status` 接口返回赞助状态，前端根据该状态决定是否展示门控页面；各业务接口也通过 `requireVip2` 函数进行二次校验。

## 目标

在不删除现有校验框架的前提下，让 `ai-assistant-web` 对所有用户可用，便于后续根据运营需要随时恢复 VIP 限制。

## 方案概述

采用**保留校验接口但总是返回通过**的方案：

- `/api/vip-status` 接口固定返回 `{ ok: true, vipLevel: 2, active: true }`
- 业务接口中移除 `requireVip2(w)` 调用
- 保留 `requireVip2` / `vipDeniedMessage` 函数，便于后续恢复

## 改动范围

### 后端

文件：`ai-assistant-web/server.go`

改动点：
1. `vipStatus` 函数直接返回 `ok: true`
2. `getAIConfigs` 中移除 `requireVip2` 调用
3. `getPrompts` 中移除 `requireVip2` 调用
4. `session` 中移除 `requireVip2` 调用
5. `summaryChatStream` 中移除 `requireVip2` 调用
6. `shareText` 中移除 `requireVip2` 调用

保留：
- `/api/vip-status` 路由
- `requireVip2` 函数
- `vipDeniedMessage` 函数

### 前端

无需改动。`/api/vip-status` 返回 `ok: true` 后，门控页面自动放行。

## 数据与配置影响

- 不涉及数据库 schema 变更
- 不影响 `data.EffectiveSponsorVipLevel()` 实现
- 不影响桌面端 VIP 逻辑

## 风险与回滚

- 风险：Web AI 助手对任意可访问该服务的用户开放
- 回滚：恢复 `vipStatus` 原逻辑，并在各业务接口重新加上 `requireVip2` 调用即可

## 验收标准

- [ ] `/api/vip-status` 返回 `ok: true`
- [ ] 未赞助用户也能正常调用 `getAIConfigs`、`getPrompts`、`session`、`summaryChatStream`、`shareText`
- [ ] 现有赞助校验相关函数未被删除
