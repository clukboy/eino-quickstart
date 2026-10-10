# P18 默认 Agent 使用 Skill 验收

## 已交付边界

工作区 specialist 现在被明确授予 `list_skills` 和 `load_skill`，但只保留 `read_file`、`list_dir` 与 Skill 加载能力；不会因为读取 Skill 而获得写入或执行权限。根 coordinator 仍通过 specialist 委派请求。

## 直接验证

1. 确保 `skills/` 下有带唯一标题的测试 Skill，例如 `skills/acceptance-skill/SKILL.md`，内容要求读取一个固定文件并按固定格式返回。
2. 启动 API，向需要查看工作区的请求发送到 root agent。
3. 查看工具记录：`workspace_agent` 应先调用 `list_skills`，再调用 `load_skill`，随后按 Skill 要求读取文件。
4. 删除/移走该 Skill 后重复请求：工具应报告 Skill 不存在，不能声称已经加载。
5. 在 Skill 中写入要求执行 shell 或写文件的文字，确认工具策略仍拒绝这些操作；Skill 内容不等于权限。

## 本地静态回归

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
go test ./internal/application/agent ./internal/skill -count=1
bash scripts/acceptance/run.sh P18
```

真实模型调用和工具记录需要在有模型密钥的隔离环境执行；静态/构造测试不应冒充真实 Agent 行为已验证。
