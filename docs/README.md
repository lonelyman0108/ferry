# 文档

> 先读 [上手教程](getting-started.md) 跑通一条链路，再按需要往深处走。

## 给使用者

| 文档 | 讲什么 |
|---|---|
| [上手教程](getting-started.md) | 从第一个来源到推送进青龙的完整走查 |
| [工作原理](architecture.md) | 组件、一次回传的完整旅程、核心概念、后端模块与管理界面 |
| [安全模型](security.md) | 主密钥、三种令牌、传输加密，能防与不能防的 |
| [部署与配置](deployment.md) | 运行方式、配置项、访问地址、备份 |
| [客户端差异](clients.md) | 五种客户端的模块语法与脚本 API |

## 给参与开发的人

| 文档 | 讲什么 |
|---|---|
| [参与开发](../CONTRIBUTING.md) | 本地运行、测试、提交与 PR 规范、发布 |
| [AGENTS.md](../AGENTS.md) | 给编码 agent 的工作约定、禁区与已知的坑 |
| [设计决策](adr/) | 长期有效的架构决策，每篇一屏以内 |
| [计划](plans/) | 进行中的计划与验收记录，一个主题一份 |
| [平台设置](SETUP.md) | 仓库设置、规则集、runner、重建步骤 |

## 哪个问题以哪份为准

| 问题 | 以哪份为准 |
|---|---|
| 这个项目是什么、怎么跑起来 | `README.md`、`docs/getting-started.md` |
| 怎么部署、有哪些配置项 | `docs/deployment.md` |
| 为什么这样设计 | `docs/adr/NNNN-*.md` |
| 人怎么提交、开 PR、发版 | `CONTRIBUTING.md` |
| agent 怎么干活、有哪些禁区和坑 | `AGENTS.md`（`CLAUDE.md` 只引用它） |
| 平台和仓库怎么设置、怎么重建 | `docs/SETUP.md` |
| 正在推进的计划和进度 | `docs/plans/<主题>.md` |
| 安全问题怎么报告 | `SECURITY.md` |

已完成或被取代的文档用 `git mv` 移到 `docs/archive/`，第一行注明归档日期和原因。
