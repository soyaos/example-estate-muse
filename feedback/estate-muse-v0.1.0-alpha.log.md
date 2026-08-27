# EstateMuse v0.1.0 Alpha 两周试用验收报告

> 结论：**BLOCKED**。本报告仅汇总匿名结构化证据，不包含身份、联系方式、原始输入或生成内容正文。

- 报告日期：2026-08-27
- 证据目录：`feedback/evidence`
- 真人参与者：0 / 5
- 会话总数：0
- 自动生产链路：通过

## 硬门槛

| 指标 | 结果 |
| --- | --- |
| 5 名真实房产作者 × 至少 14 天 | 未满足 |
| 每人至少 3 次使用、一次 500 行 XLSX、一次逐行 Action | 未满足 |
| 500 行生成 ≤ 300000 ms | p50 —；p95 — |
| 图文/视频 Action ≤ 60000 ms | p50 —；p95 — |
| 跨进程重启状态保留 | 通过 |

## 参与者覆盖

| 编号 | 画像 | 覆盖天数 | 会话 | Action | 最终选择 |
| --- | --- | ---: | ---: | ---: | --- |
| EM-01 | — | 0 | 0 | 0 | 缺少证据 |
| EM-02 | — | 0 | 0 | 0 | 缺少证据 |
| EM-03 | — | 0 | 0 | 0 | 缺少证据 |
| EM-04 | — | 0 | 0 | 0 | 缺少证据 |
| EM-05 | — | 0 | 0 | 0 | 缺少证据 |

## 匿名摘要

尚无真实作者反馈；不生成替代性评价。

## P0 / P1 / P2

| 编号 | 严重度 | 状态 | Linear |
| --- | --- | --- | --- |
| — | — | — | — |

## 阻塞项

- [ ] feedback/evidence/participants/EM-01.json: missing real-participant evidence
- [ ] feedback/evidence/participants/EM-02.json: missing real-participant evidence
- [ ] feedback/evidence/participants/EM-03.json: missing real-participant evidence
- [ ] feedback/evidence/participants/EM-04.json: missing real-participant evidence
- [ ] feedback/evidence/participants/EM-05.json: missing real-participant evidence
- [ ] feedback/evidence/issue-register.json.review_completed: must be true
- [ ] trial-wide evidence: no successful generate_post action recorded
- [ ] trial-wide evidence: no successful generate_video action recorded

## 复现命令

```bash
python3 feedback/acceptance.py verify
```

只有该命令返回 0 时，APP-1701 和 APP-506 才具备关闭条件。自动测试通过不能替代 5 名真人的两周试用。
