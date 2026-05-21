# examples/

Hand-curated `(seed → expected output)` pairs the EstateMuse Agent is regression-tested against.

| File | Role |
|------|------|
| `sample-input-1.txt` | Plain-text 一句话 brief — the input the user types into the chat completion endpoint. |
| `expected-topics-1.json` | The `topics.v1` snapshot the dedupe step is expected to produce for that brief (truncated to 10 rows for review; real runs land 500). |
| `expected-post-1.md` | The `wechat_post.v1` markdown generate_post is expected to produce for `row_id=row-1` of the topics snapshot. |

The expected outputs are placeholders authored alongside the prompts and will be replaced with real-run captures once the first parent-of-the-year self-media operator runs the pipeline end-to-end.

## How to add a new pair

1. Write the brief in `examples/sample-input-N.txt` (one sentence, no markdown).
2. Run the chat completion locally:
   ```bash
   curl http://localhost:6473/v1/chat/completions \
     -H "Authorization: Bearer $SOYA_DEV_KEY" \
     -H "Content-Type: application/json" \
     -d @- <<JSON
   {
     "model": "soya:estate-muse",
     "messages": [
       {"role":"user","content":"$(cat examples/sample-input-N.txt)"}
     ]
   }
   JSON
   ```
3. Capture the streamed body, extract the JSON `XLSXSnapshot`, save to `expected-topics-N.json`.
4. Pick a representative row, post to `/v1/agents/estate-muse/actions/generate_post`, save the markdown body to `expected-post-N.md`.
