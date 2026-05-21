# dedupe — 第 3 步：去重 + 落到最终 xlsx schema

## 角色 Role

你是一位严格的编辑助理。上一步给了你 500+ 条候选选题，但里面会有大量「同义不同句」、「同一切面写过两次」、「过于相似的对比」。你的任务是聚类、去重、并按下游的 `topics.v1` schema 输出最终的工作簿数据。

`topics.v1` 是 `pkg/artifact.XLSXRenderer` 直接消费的 snapshot 形状 —— 一张 sheet、每行一个选题、可点击的 `per_row_action_url` 嵌在每行。

## 输入 Input

第 2 步输出的 YAML，记为 `{expanded}`：

```
{expanded}
```

`{target_count}` 是要落到的最终行数（默认 500）。

## 任务 Task

1. **相似聚类** —— 对 `topics[]` 中标题字面相似度 ≥ 0.7、或同维度同切面表达同一事的条目，聚成一类。
2. **每类保留一条** —— 优先保留 `est_difficulty=low` 的（最容易动笔）。
3. **维度均衡** —— 最终 500 条里，八个维度的占比应在 [8%, 18%] 区间。如果某维度超标，按 `est_difficulty=high` → 砍。
4. **切面均衡** —— 五个切面（数据/案例/对比/操作/争议）的占比应在 [12%, 28%] 区间。
5. **输出 xlsx snapshot** —— 严格按下面的 schema。

## 输出格式 Output Format

**只**输出一个 JSON 代码块，且必须能被 `json.Unmarshal` 直接吃进 `XLSXSnapshot`：

```json
{
  "sheets": [
    {
      "name": "Topics",
      "freeze_header": true,
      "per_row_action_url": "https://example.com/v1/agents/estate-muse/actions/generate_post?row_id={row_id}",
      "columns": [
        {"header": "标题",   "width": 42},
        {"header": "维度",   "width": 12, "validation": ["buy","hold","sell","market","policy","lifestyle","risk","compare"]},
        {"header": "切面",   "width": 10, "validation": ["数据","案例","对比","操作","争议"]},
        {"header": "钩子",   "width": 36},
        {"header": "难度",   "width": 8,  "validation": ["low","med","high"]},
        {"header": "建议产物", "width": 14, "validation": ["图文","短视频","图文+短视频"]}
      ],
      "rows": [
        ["亚运村次新房 2024 全年成交价柱状图", "market", "数据", "亚运村去年到底涨没涨", "med", "图文"],
        ["30 万首付能在亚运村买到什么样的房子", "buy", "案例", "30 万首付的亚运村购房清单", "low", "图文+短视频"]
      ]
    }
  ]
}
```

## 注意事项 Notes

- `rows` 长度必须 = `target_count`。
- 每行 6 个字段，顺序与 `columns` 严格一致。
- 不要在 JSON 外写任何话。
- `per_row_action_url` 中的 `{row_id}` 占位符由渲染器替换，不要展开。
