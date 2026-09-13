package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// TestProductionBinary_EstateMuseTrialPath exercises the operator-visible
// path with a real SoyaOS process and the real EstateMuse pack:
//
//	build binary → build Pack → deploy → generate 500-row XLSX → action
//	→ stop → restart with the same data directory → action from saved row
//
// The only test double is the external OpenAI-compatible model endpoint.
func TestProductionBinary_EstateMuseTrialPath(t *testing.T) {
	soyaRoot := findSoyaOSRoot(t)
	testRoot := t.TempDir()
	bin := filepath.Join(testRoot, "soyaos")
	// Cold toolchain builds on a loaded machine need a separate budget.
	// This does not relax the real 5-minute workbook generation assertion.
	// The E2E module may run with GOWORK=off; the sibling binary deliberately
	// builds its real multi-module workspace, independent of the parent's env.
	runCommandWithTimeout(t, 10*time.Minute, soyaRoot, []string{"GOWORK=" + filepath.Join(soyaRoot, "go.work")}, "go", "build", "-p", "1", "-o", bin, "./cmd/soyaos")

	packCopy := filepath.Join(testRoot, "estate-muse-pack")
	copyPackTree(t, packDir(t), packCopy)
	runCommand(t, testRoot, nil, bin, "pack", "validate", packCopy)
	runCommand(t, testRoot, nil, bin, "agent", "build", packCopy)
	spk := filepath.Join(packCopy, "dist", "estate-muse-0.1.0-alpha.0.spk")

	mock := newProductionMock(t)
	env := []string{
		"SOYA_MODEL_API_KEY=sk-mock-upstream",
		"SOYA_MODEL_BASE_URL=" + mock.server.URL,
		"SOYA_MODEL_DEFAULT=estate-muse-test-model",
	}
	dataDir := filepath.Join(testRoot, "data")
	gatewayAddr := freeAddress(t)
	rpcAddr := freeAddress(t)

	process := startSoyaOSProcess(t, bin, dataDir, gatewayAddr, rpcAddr, env)
	t.Cleanup(func() { process.stop(t) })
	runCommand(t, testRoot, nil, bin, "agent", "deploy", spk, "--rpc", "http://"+rpcAddr)

	xlsxPath := filepath.Join(testRoot, "topics.xlsx")
	started := time.Now()
	runCommand(t, testRoot, nil, bin, "agent", "invoke", "estate-muse", "杭州亚运村二手房 500 条选题",
		"--listen", "http://"+gatewayAddr,
		"--key", "sk-soya-dev-local",
		"--artifact", "xlsx",
		"--max-tokens", "65536",
		"--expected-rows", "500",
		"--schema", "topics.v1",
		"--output", xlsxPath,
	)
	if elapsed := time.Since(started); elapsed > 5*time.Minute {
		t.Fatalf("500-row generation took %s, want <= 5m", elapsed)
	}
	assertWorkbookRows(t, xlsxPath, 500)

	// A rejected selection must not replace the previous saved workbook rows.
	mock.mu.Lock()
	mock.rejectSelection = true
	mock.mu.Unlock()
	requestBody := strings.NewReader(`{"model":"soya:estate-muse","messages":[{"role":"user","content":"test rejected replacement"}]}`)
	request, err := http.NewRequest(http.MethodPost, "http://"+gatewayAddr+"/v1/chat/completions", requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer sk-soya-dev-local")
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("499-index selection unexpectedly succeeded")
	}
	mock.mu.Lock()
	mock.rejectSelection = false
	mock.mu.Unlock()

	actionStarted := time.Now()
	status, body := postProductionAction(t, gatewayAddr, "generate_post", "伪造标题")
	if status != http.StatusOK {
		t.Fatalf("generate_post status=%d body=%s", status, body)
	}
	if elapsed := time.Since(actionStarted); elapsed > time.Minute {
		t.Fatalf("generate_post took %s, want <= 60s", elapsed)
	}
	assertSavedRowReachedUpstream(t, mock.lastActionPayload(), "选题 003")

	videoStarted := time.Now()
	status, body = postProductionAction(t, gatewayAddr, "generate_video", "短视频伪造标题")
	if status != http.StatusOK {
		t.Fatalf("generate_video status=%d body=%s", status, body)
	}
	if elapsed := time.Since(videoStarted); elapsed > time.Minute {
		t.Fatalf("generate_video took %s, want <= 60s", elapsed)
	}
	assertSavedRowReachedUpstream(t, mock.lastActionPayload(), "选题 003")

	process.stop(t)
	process = startSoyaOSProcess(t, bin, dataDir, gatewayAddr, rpcAddr, env)

	status, body = postProductionAction(t, gatewayAddr, "generate_post", "重启后伪造标题")
	if status != http.StatusOK {
		t.Fatalf("post-restart action status=%d body=%s", status, body)
	}
	assertSavedRowReachedUpstream(t, mock.lastActionPayload(), "选题 003")
}

type productionMock struct {
	server          *httptest.Server
	mu              sync.Mutex
	action          []string
	rejectSelection bool
}

func newProductionMock(t *testing.T) *productionMock {
	t.Helper()
	m := &productionMock{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		system := ""
		user := ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				system = message.Content
			}
			if message.Role == "user" {
				user = message.Content
			}
		}

		response := `{"stage":"ok"}`
		m.mu.Lock()
		reject := m.rejectSelection
		m.mu.Unlock()
		switch {
		case strings.Contains(system, "# plan_post"):
			response = "MOCK_EDITORIAL_PLAN"
		case strings.Contains(system, "# EstateMuse 独立稿件审查"):
			var review struct {
				Request map[string]any `json:"request"`
				Draft   string         `json:"draft"`
			}
			if json.Unmarshal([]byte(user), &review) != nil || review.Request["row_id"] == nil || !strings.Contains(review.Draft, "ACTION_OK") {
				http.Error(w, "invalid review envelope", 400)
				return
			}
			response = `{"approved":true,"findings":[],"checks":[{"id":"business_context","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"factual_claims","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"method_logic","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"format","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"safety","passed":true,"reason":"Protocol fixture; not factual evaluation"}]}` // tests protocol wiring only
		case strings.Contains(system, "# expand"):
			var batch struct {
				TargetCount int    `json:"target_count"`
				BatchIndex  int    `json:"batch_index"`
				Partition   string `json:"partition_value"`
			}
			if err := json.Unmarshal([]byte(user), &batch); err != nil || batch.TargetCount < 1 || batch.BatchIndex < 1 {
				http.Error(w, "invalid batch envelope", http.StatusBadRequest)
				return
			}
			response = productionCandidates(t, (batch.BatchIndex-1)*100+1, batch.TargetCount, batch.Partition)
			if reject {
				response = strings.ReplaceAll(response, "选题", "不可持久化的失败候选")
			}
		case strings.Contains(system, "# dedupe"):
			count := 500
			if reject {
				count = 499
			}
			indices := make([]int, count)
			for i := range indices {
				indices[i] = i + 1
			}
			body, err := json.Marshal(map[string]any{"indices": indices})
			if err != nil {
				t.Fatal(err)
			}
			response = string(body)
		case strings.Contains(system, "# generate_post"):
			m.mu.Lock()
			m.action = append(m.action, user)
			m.mu.Unlock()
			response = "# 生产链路测试图文\n\nACTION_OK\n" + strings.Repeat("核对清单与现场条件。", 80)
		case strings.Contains(system, "# generate_video"):
			m.mu.Lock()
			m.action = append(m.action, user)
			m.mu.Unlock()
			response = "# 生产链路测试短视频\n\n## 口播全文\n" + strings.Repeat("核对清单与现场条件。", 10) + "\n## 待核实\nACTION_OK\n" + mockNarrationTable("核对清单与现场条件。", 3, 4, 3)
		}
		writeSSE(t, w, response)
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *productionMock) lastActionPayload() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.action) == 0 {
		return ""
	}
	return m.action[len(m.action)-1]
}

func productionCandidates(t *testing.T, start, count int, dimension string) string {
	t.Helper()
	rows := make([][]any, 0, count)
	for i := start; i < start+count; i++ {
		rows = append(rows, []any{
			fmt.Sprintf("选题 %03d：亚运村看房应确认什么？", i),
			dimension,
			"数据",
			fmt.Sprintf("拍摄前需核实：第 %03d 个观察条件", i),
			"med",
			"图文",
		})
	}
	payload := map[string]any{"rows": rows}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal production snapshot: %v", err)
	}
	return string(body)
}

func writeSSE(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	runes := []rune(content)
	for len(runes) > 0 {
		size := 1000
		if len(runes) < size {
			size = len(runes)
		}
		frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"delta":         map[string]any{"content": string(runes[:size])},
			"finish_reason": nil,
		}}})
		fmt.Fprintf(w, "data: %s\n\n", frame)
		if flusher != nil {
			flusher.Flush()
		}
		runes = runes[size:]
	}
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

type runningSoyaOS struct {
	cmd  *exec.Cmd
	done chan error
	logs *lockedBuffer
}

func startSoyaOSProcess(t *testing.T, bin, dataDir, gatewayAddr, rpcAddr string, env []string) *runningSoyaOS {
	t.Helper()
	cmd := exec.Command(bin, "start", "--listen", gatewayAddr, "--rpc", rpcAddr, "--data-dir", dataDir)
	cmd.Env = append(os.Environ(), env...)
	logs := &lockedBuffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start soyaos: %v", err)
	}
	p := &runningSoyaOS{cmd: cmd, done: make(chan error, 1), logs: logs}
	go func() { p.done <- cmd.Wait() }()
	waitForHealth(t, "http://"+gatewayAddr+"/healthz", p)
	return p
}

func (p *runningSoyaOS) stop(t *testing.T) {
	t.Helper()
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-p.done:
		if err != nil {
			t.Logf("soyaos stopped with %v; logs:\n%s", err, p.logs.String())
		}
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	p.cmd = nil
}

func waitForHealth(t *testing.T, url string, process *runningSoyaOS) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-process.done:
			t.Fatalf("soyaos exited before health check: %v\n%s", err, process.logs.String())
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("soyaos health timed out: %s\n%s", url, process.logs.String())
}

func postProductionAction(t *testing.T, gatewayAddr, action, callerTitle string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"row_id":  "row-17",
		"payload": map[string]any{"title": callerTitle, "original_request": "篡改原始需求", "option": "production-e2e"},
	})
	req, _ := http.NewRequest(http.MethodPost,
		"http://"+gatewayAddr+"/v1/agents/estate-muse/actions/"+action,
		bytes.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer sk-soya-dev-local")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post production action: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func assertSavedRowReachedUpstream(t *testing.T, payload, wantTitle string) {
	t.Helper()
	if !strings.Contains(payload, wantTitle) {
		t.Fatalf("saved row title missing from action payload: %s", payload)
	}
	if strings.Contains(payload, "伪造标题") {
		t.Fatalf("caller-supplied title overrode persisted row: %s", payload)
	}
	if !strings.Contains(payload, "杭州亚运村二手房 500 条选题") || strings.Contains(payload, "篡改原始需求") {
		t.Fatalf("original request not preserved in action: %s", payload)
	}
}

func assertWorkbookRows(t *testing.T, path string, wantRows int) {
	t.Helper()
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open generated workbook: %v", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("Topics")
	if err != nil {
		t.Fatalf("read Topics sheet: %v", err)
	}
	if len(rows) != wantRows+1 {
		t.Fatalf("Topics rows=%d, want %d data rows + header", len(rows), wantRows)
	}
}

func findSoyaOSRoot(t *testing.T) string {
	t.Helper()
	pack := packDir(t)
	for _, candidate := range []string{
		filepath.Join(pack, "..", "soyaos"),
		filepath.Join(pack, "..", "..", "soyaos"),
	} {
		candidate, _ = filepath.Abs(candidate)
		if _, err := os.Stat(filepath.Join(candidate, "go.work")); err == nil {
			return candidate
		}
	}
	t.Fatal("cannot locate sibling SoyaOS checkout (expected ../soyaos or ../../soyaos)")
	return ""
}

func copyPackTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "e2e") {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		t.Fatalf("copy pack tree: %v", err)
	}
}

func runCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	return runCommandWithTimeout(t, 2*time.Minute, dir, env, name, args...)
}

func runCommandWithTimeout(t *testing.T, timeout time.Duration, dir string, env []string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GOTOOLCHAIN=go1.23.12"}, env...)...)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("%s %s: command deadline %s exceeded: %v\n%s", name, strings.Join(args, " "), timeout, ctx.Err(), raw)
		}
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, raw)
	}
	return string(raw)
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
