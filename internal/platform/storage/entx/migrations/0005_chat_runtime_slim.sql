-- 0005_chat_runtime_slim
--
-- chat 链路收敛后的清理：
--
--   1. agent_runs 与 chat_turns 逐列重复（同样的 session_id / 提问 / 状态 /
--      完成时间），是审批与断点续跑时代留下的「执行租约」表。租约逻辑
--      （ClaimResume / ReleaseResume / MarkInterrupted）早已删除，这张表实际
--      只被写入 4 个字段，其余 checkpoint_id / approval_id / steps / tool_calls /
--      tokens_* / cost_micros / latency_ms 从未写入过一列。
--   2. run_spans 与 run_retrieval_hits 只被控制台读过、从来没有写入路径 ——
--      没有 span 上报，也没有检索命中落库，两张表必然恒空。
--
-- 控制台的「运行记录」改为直接读 chat_turns，响应形状不变（run_id 即 turn_id），
-- 所以删表不会让控制台页面变空。
--
-- ⚠️ 破坏性：三张表的存量数据不再保留。执行前确认不再需要历史运行记录
-- （会话内容仍在 session_messages 里，不受影响）。
--
-- 注意 chat_turns 上的 approval_id / checkpoint_id 两列**没有**在这里删除：
-- MigrationPlan 的守卫禁止删列（见 migration_test.go，它连注释里的关键字都扫），
-- 而这两列是 nullable、没有任何读路径，留着不占空间也不影响查询。ent schema
-- 已经不再声明它们，additive reconcile 不会重建；要彻底清掉请由 DBA 确认后手工执行。

DROP TABLE IF EXISTS run_retrieval_hits CASCADE;
DROP TABLE IF EXISTS run_spans CASCADE;
DROP TABLE IF EXISTS agent_runs CASCADE;
