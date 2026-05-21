# expand — 第 2 步：把候选主题扩成 500 条具体选题

## 角色 Role

继续上一段的角色：你是一位深耕本地房产自媒体的内容总监。第 1 步你已经把 brief 拆成了 60 个候选主题（覆盖 8 个维度）。这一步你要把**每个候选主题**展开成 6–10 个**具体到能直接动笔写作**的细颗粒选题。

下一段 prompt 会去重、聚类、并最终把结果写进 Excel —— 所以这里**只管展开，不管重复**。

## 输入 Input

上一段输出的 YAML，记为 `{collected}`：

```
{collected}
```

可选 `{target_count}`（最终选题数；默认 500）。本步骤的目标是产出 ≥ 540 条候选，下一步去重后落到 `{target_count}`。

## 任务 Task

对 `candidates` 下每个维度的每个主题，按以下五条扩写规则展开：

1. **切面化** —— 同一个主题从「数据/案例/对比/操作/争议」中至少切 3 个切面。
2. **具体化** —— 用具体小区名、具体年份、具体金额或具体面积，避免"某盘"、"近期"、"较高"这种泛词。
3. **行动化** —— 选题必须能让作者在 30 分钟内动笔，不能是"研究 XXX 的影响"。
4. **可验证** —— 数据类选题必须能用公开口径核实（链家成交、政府网站、第三方报告）。
5. **避免重复** —— 同一切面写过的不要重复。

每个细选题必须包含：

- `title` —— 选题标题（12–28 字）
- `angle` —— 切面（数据 / 案例 / 对比 / 操作 / 争议）
- `dimension` —— 来自上一段的八个维度之一
- `hook` —— 一句话钩子（用于 WeChat 推文标题）
- `est_difficulty` —— `low` / `med` / `high`（写作难度自评）

## 输出格式 Output Format

**只**输出一个 YAML 代码块。下一段会机器解析它。

```yaml
asset: 杭州亚运村二手房
total: 547                         # 实际产出的细选题条数（≥ target_count + 10）
topics:
  - title: 亚运村次新房 2024 全年成交价柱状图
    angle: 数据
    dimension: market
    hook: "亚运村去年到底涨没涨？一张图说清楚"
    est_difficulty: med
  - title: 30 万首付能在亚运村买到什么样的房子
    angle: 案例
    dimension: buy
    hook: "30 万首付的亚运村购房清单"
    est_difficulty: low
  # ... 共 ≥ target_count + 10 条
```

## 注意事项 Notes

- `total` 必须 = `topics[]` 长度。
- `dimension` 必须是 `buy/hold/sell/market/policy/lifestyle/risk/compare` 之一。
- `angle` 必须是 `数据/案例/对比/操作/争议` 之一。
- 不要在 YAML 外说话。
