-- 0004: drop tables that belonged to the local-workstation agent runtime.
--
-- 删除的四类能力（审批流、断点续跑、模型供应商、模型路由）在服务器部署里没有
-- 入口，也不再有任何 Go 代码读写这些表。保留空表只会让后来的人误以为它们还在
-- 生效，所以这里显式删除。
--
-- ⚠️ 这一步是不可逆的：执行前请确认这些表里的历史数据不需要保留。
-- 顺序不能换：models 有指向 model_providers 的外键。
DROP TABLE IF EXISTS models CASCADE;
DROP TABLE IF EXISTS model_providers CASCADE;
DROP TABLE IF EXISTS model_routings CASCADE;
DROP TABLE IF EXISTS checkpoints CASCADE;
DROP TABLE IF EXISTS approvals CASCADE;
DROP TABLE IF EXISTS approval_policies CASCADE;
