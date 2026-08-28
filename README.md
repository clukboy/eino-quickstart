# Eino Harness

基于 [Eino](https://github.com/cloudwego/eino) ADK 的多 Agent 软件工程 Harness。入口服务位于 `cmd/server`，默认由根协调 Agent 将请求分派给知识库、工作区读取和受控自动化专项 Agent。

## 开发文档

- [新增 Agent、工具与 Skill](docs/agent-development.md)：专项 Agent 的接入步骤、Eino Tool 编写方式、Skill 配置与编写规范。

## 主要目录

| 路径 | 职责 |
| --- | --- |
| `internal/agent` | 根协调器和专项 Agent 的定义与装配 |
| `internal/tool` | 工具实现、注册表与风险分级 |
| `internal/skill` | `SKILL.md` 发现与按需加载 |
| `internal/middleware` | 工具白名单、审批与输出截断 |
| `configs/config.yaml` | 服务、模型、工作区、Skill 和安全配置 |
