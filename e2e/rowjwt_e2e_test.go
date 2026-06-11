// rowjwt_e2e_test.go — 行级 JWT 鉴权端到端测试 (APP-1059, 验收项 2).
//
// Full-stack harness, no fakes in the auth path:
//
//	real estate-muse pack (../soyapack.yaml + prompts/, loaded via
//	soyapack.LoadFromFile → Validate → kernel.RegisterFromPack)
//	  ↓
//	real kernel + real bbolt-backed sk-soya verifier (auth.StoreBacked)
//	  + real RowTokenSigner persisted via LoadOrCreateRowTokenSigner
//	  ↓
//	real openaicompat gateway served over a real TCP listener
//	(httptest.NewServer) — requests travel through net/http exactly as
//	production traffic does
//	  ↓
//	mock upstream LLM (OpenAI-compatible SSE server) — the only test
//	double, standing in for api.openai.com; it also captures request
//	bodies so tests can assert what reached the model layer.
//
// Scenario matrix (issue 验收: 正向 + 反向):
//
//	正向  own-row token → 200, action runs, prompt file + row envelope
//	      reach the upstream verbatim; sk-soya bearer also accepted;
//	      token survives a signer restart; 24h-cap TTL boundary mints.
//	反向  row substitution (row-42 token → row-99 body)   → 401
//	      action substitution (post token → video action) → 401
//	      agent substitution (other-agent token)          → 401
//	      expired token                                   → 401
//	      forged signature (wrong secret)                 → 401
//	      garbage / missing credential                    → 401
//	      privilege escalation to /v1/chat/completions    → 401
//	      every rejection happens BEFORE any upstream LLM call.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soyaos/soyaos/pkg/auth"
	"github.com/soyaos/soyaos/pkg/kernel"
	"github.com/soyaos/soyaos/pkg/llmcall"
	"github.com/soyaos/soyaos/pkg/openaicompat"
	"github.com/soyaos/soyaos/pkg/soyapack"
	"github.com/soyaos/soyaos/pkg/store"
)

const (
	agentSlug   = "estate-muse"
	actionPost  = "generate_post"
	actionVideo = "generate_video"
	mockModel   = "mock-writer-model"
	mockAnswer  = "【MOCK】亚运村次新房图文已生成"
)

// upstreamCapture records every chat/completions body the mock upstream
// receives, so tests can assert (a) the real prompt file content reached
// the model and (b) rejected requests never reached it at all.
type upstreamCapture struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (c *upstreamCapture) add(body map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, body)
}

func (c *upstreamCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func (c *upstreamCapture) last() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return nil
	}
	return c.bodies[len(c.bodies)-1]
}

// harness is one fully-booted EstateMuse stack.
type harness struct {
	gatewayURL string
	devKey     string
	signer     *auth.RowTokenSigner
	signerPath string
	upstream   *upstreamCapture
}

// startHarness boots the full stack. Uses t.Setenv for the SOYA_MODEL_*
// upstream config (read by RegisterFromPack via llmcall.ResolveConfig),
// so harness tests must not call t.Parallel().
func startHarness(t *testing.T) *harness {
	t.Helper()
	dataDir := t.TempDir()

	// --- mock upstream LLM (OpenAI-compatible, SSE) ----------------------
	capture := &upstreamCapture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		capture.add(body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", mockAnswer)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)

	t.Setenv(llmcall.EnvAPIKey, "sk-mock-upstream-key")
	t.Setenv(llmcall.EnvBaseURL, upstream.URL)
	t.Setenv(llmcall.EnvModel, mockModel)

	// --- real persistence + auth (the cmd/soyaos wiring) -----------------
	soyaStore, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = soyaStore.Close() })
	keys := auth.NewStoreBacked(soyaStore)
	devKey := keys.SeedDevKey()

	signerPath := filepath.Join(dataDir, "rowtoken-key")
	signer, err := auth.LoadOrCreateRowTokenSigner(signerPath)
	if err != nil {
		t.Fatalf("LoadOrCreateRowTokenSigner: %v", err)
	}

	// --- real kernel + the real pack from this repo ----------------------
	m, err := soyapack.LoadFromFile(filepath.Join(packDir(t), "soyapack.yaml"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if err := soyapack.Validate(m); err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	k := kernel.New()
	if err := k.RegisterFromPack(m, packDir(t)); err != nil {
		t.Fatalf("RegisterFromPack: %v", err)
	}

	// --- real gateway over a real TCP listener ----------------------------
	gw := &openaicompat.Server{Kernel: k, Verifier: keys, RowTokens: signer}
	mux := http.NewServeMux()
	mux.Handle("/v1/", gw.Handler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &harness{
		gatewayURL: srv.URL,
		devKey:     devKey,
		signer:     signer,
		signerPath: signerPath,
		upstream:   capture,
	}
}

// postAction fires POST /v1/agents/{slug}/actions/{action} with the given
// bearer credential and returns (statusCode, decodedBody).
func (h *harness) postAction(t *testing.T, bearer, slug, action, rowID string) (int, map[string]any) {
	t.Helper()
	payload := map[string]any{
		"row_id": rowID,
		"payload": map[string]any{
			"title":     "亚运村次新房 2024 全年成交价柱状图",
			"dimension": "market",
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/v1/agents/%s/actions/%s", h.gatewayURL, slug, action), bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST action: %v", err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func (h *harness) mint(t *testing.T, slug, action, row string, ttl time.Duration) string {
	t.Helper()
	tok, err := h.signer.Mint(slug, action, row, "sk-soya-dev-l", ttl)
	if err != nil {
		t.Fatalf("mint row token: %v", err)
	}
	return tok
}

// --- 正向用例 ---------------------------------------------------------------

func TestE2E_RowToken_OwnRow_AllowedAndActionRuns(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-17", time.Hour)

	status, body := h.postAction(t, tok, agentSlug, actionPost, "row-17")
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%v, want 200", status, body)
	}
	if body["status"] != "done" || body["row_id"] != "row-17" || body["action_id"] != actionPost {
		t.Fatalf("action result mismatch: %v", body)
	}
	out, _ := body["output"].(map[string]any)
	if out == nil || out["content"] != mockAnswer {
		t.Fatalf("output.content = %v, want mock answer", out)
	}

	// The REAL prompt file must have reached the upstream as the system
	// message, and the user message must carry the row envelope.
	upstreamBody := h.upstream.last()
	if upstreamBody == nil {
		t.Fatal("upstream never called")
	}
	if got := upstreamBody["model"]; got != mockModel {
		t.Fatalf("upstream model = %v, want %q (virtual id must not leak)", got, mockModel)
	}
	msgs, _ := upstreamBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("upstream messages = %d, want 2 (system + user)", len(msgs))
	}
	wantPrompt, err := os.ReadFile(filepath.Join(packDir(t), "prompts", "generate_post.md"))
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != string(wantPrompt) {
		t.Fatal("system message is not the verbatim prompts/generate_post.md body")
	}
	user := msgs[1].(map[string]any)
	if !strings.Contains(user["content"].(string), `"row_id":"row-17"`) {
		t.Fatalf("user envelope missing row_id: %v", user["content"])
	}
}

func TestE2E_RowToken_VideoAction_Allowed(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionVideo, "row-42", time.Hour)
	status, body := h.postAction(t, tok, agentSlug, actionVideo, "row-42")
	if status != http.StatusOK || body["action_id"] != actionVideo {
		t.Fatalf("status=%d body=%v, want 200 generate_video", status, body)
	}
}

func TestE2E_SkSoyaBearer_AllowedOnActions(t *testing.T) {
	h := startHarness(t)
	status, body := h.postAction(t, h.devKey, agentSlug, actionPost, "row-3")
	if status != http.StatusOK {
		t.Fatalf("dev key status = %d body=%v, want 200", status, body)
	}
}

func TestE2E_RowToken_SurvivesSignerRestart(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-8", time.Hour)

	// Reload the signer from the same on-disk secret — the restart path.
	reloaded, err := auth.LoadOrCreateRowTokenSigner(h.signerPath)
	if err != nil {
		t.Fatalf("reload signer: %v", err)
	}
	claims, err := reloaded.Verify(tok)
	if err != nil {
		t.Fatalf("token minted before restart no longer verifies: %v", err)
	}
	if claims.AgentSlug != agentSlug || claims.RowID != "row-8" {
		t.Fatalf("claims drift after restart: %+v", claims)
	}
}

func TestE2E_RowToken_TTLCapBoundary(t *testing.T) {
	h := startHarness(t)
	// Exactly the 24h cap must mint…
	if _, err := h.signer.Mint(agentSlug, actionPost, "row-1", "pfx", auth.MaxRowTokenTTL); err != nil {
		t.Fatalf("Mint at exactly 24h cap failed: %v", err)
	}
	// …one nanosecond over must not.
	if _, err := h.signer.Mint(agentSlug, actionPost, "row-1", "pfx", auth.MaxRowTokenTTL+time.Nanosecond); err == nil {
		t.Fatal("Mint over the 24h cap succeeded, want ErrTTLTooLong")
	}
}

// --- 反向用例（越权必须被拒，且不得触达上游 LLM）------------------------------

// expectRejected asserts the request is rejected 401 AND that the mock
// upstream saw no traffic for it — auth must fail closed before dispatch.
func expectRejected(t *testing.T, h *harness, bearer, slug, action, row string) {
	t.Helper()
	before := h.upstream.count()
	status, body := h.postAction(t, bearer, slug, action, row)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%v, want 401", status, body)
	}
	if after := h.upstream.count(); after != before {
		t.Fatalf("rejected request still reached the upstream LLM (%d→%d calls)", before, after)
	}
}

func TestE2E_RowToken_OtherRow_Rejected(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-42", time.Hour)
	expectRejected(t, h, tok, agentSlug, actionPost, "row-99") // substitution attack
}

func TestE2E_RowToken_OtherAction_Rejected(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-42", time.Hour)
	expectRejected(t, h, tok, agentSlug, actionVideo, "row-42")
}

func TestE2E_RowToken_OtherAgent_Rejected(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, "some-other-agent", actionPost, "row-42", time.Hour)
	expectRejected(t, h, tok, agentSlug, actionPost, "row-42")
}

func TestE2E_RowToken_Expired_Rejected(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-42", time.Millisecond)
	// jwt/v5 truncates exp to whole seconds; 1.1s guarantees expiry.
	time.Sleep(1100 * time.Millisecond)
	expectRejected(t, h, tok, agentSlug, actionPost, "row-42")
}

func TestE2E_RowToken_ForgedSignature_Rejected(t *testing.T) {
	h := startHarness(t)
	forger := auth.NewRowTokenSigner(bytes.Repeat([]byte{0x42}, 32))
	tok, err := forger.Mint(agentSlug, actionPost, "row-42", "pfx", time.Hour)
	if err != nil {
		t.Fatalf("forger mint: %v", err)
	}
	expectRejected(t, h, tok, agentSlug, actionPost, "row-42")
}

func TestE2E_GarbageAndMissingCredential_Rejected(t *testing.T) {
	h := startHarness(t)
	expectRejected(t, h, "not-a-jwt-at-all", agentSlug, actionPost, "row-1")
	expectRejected(t, h, "", agentSlug, actionPost, "row-1") // no Authorization header
}

// TestE2E_RowToken_CannotEscalateToChat proves a row token is NOT a
// general API credential: the chat surface (which mints whole workbooks
// and burns real model spend) only accepts sk-soya keys.
func TestE2E_RowToken_CannotEscalateToChat(t *testing.T) {
	h := startHarness(t)
	tok := h.mint(t, agentSlug, actionPost, "row-17", time.Hour)

	body, _ := json.Marshal(map[string]any{
		"model":    "soya:estate-muse",
		"messages": []map[string]string{{"role": "user", "content": "杭州亚运村二手房 500 条选题"}},
	})
	req, _ := http.NewRequest(http.MethodPost, h.gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("row token on /v1/chat/completions = %d, want 401", resp.StatusCode)
	}
	if h.upstream.count() != 0 {
		t.Fatal("escalation attempt reached the upstream LLM")
	}
}
