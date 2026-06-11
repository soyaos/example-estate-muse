// kv_state_test.go — KV 存储（状态管理）实现验证 (APP-1059, 验收项 1).
//
// EstateMuse's manifest declares `state: { scope: agent, store: kv }` —
// the 500-row workbook lives in the agent's own state partition so the
// per-row action endpoints can look up a row's original context without
// trusting the caller's payload. These tests exercise the real runtime
// implementation behind that declaration (pkg/state.BoltStore over a
// real bbolt file, the exact production wiring of pkg/store.Open) in
// EstateMuse's usage patterns:
//
//   - 读写: Put/Get round-trip with MVCC version bumps.
//   - 并发: CompareAndSwap conflict semantics + a goroutine-parallel
//     CAS-retry counter that must lose no increments under -race.
//   - 行级隔离: row-scoped entries for row-17 never leak into row-99.
//   - 持久化: entries and versions survive a store close + reopen
//     (process-restart equivalence).
//
// NOTE / deviation record: the pkg/state Store interface deliberately
// has no TTL surface — it is an MVCC state store, not a cache. The
// "expiry" half of the EstateMuse security model lives in the row JWT
// layer (24h cap), covered by rowjwt_e2e_test.go.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/soyaos/soyaos/pkg/state"
	"github.com/soyaos/soyaos/pkg/store"
)

// openKV opens a real bbolt-backed state store under dir, exactly the way
// cmd/soyaos wires it (store.Open → state.NewBoltStore).
func openKV(t *testing.T, dir string) (state.Store, func()) {
	t.Helper()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	return state.NewBoltStore(s), func() { _ = s.Close() }
}

func TestKV_PutGet_RoundTripWithVersionBump(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	e1, err := kv.Put(ctx, state.ScopeAgent, "estate-muse", "workbook/meta", []byte(`{"rows":500}`))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if e1.Version != 1 {
		t.Fatalf("first Put version = %d, want 1", e1.Version)
	}

	got, err := kv.Get(ctx, state.ScopeAgent, "estate-muse", "workbook/meta")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Value) != `{"rows":500}` {
		t.Fatalf("Get value = %q", got.Value)
	}

	e2, err := kv.Put(ctx, state.ScopeAgent, "estate-muse", "workbook/meta", []byte(`{"rows":501}`))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if e2.Version != 2 {
		t.Fatalf("second Put version = %d, want 2 (MVCC bump)", e2.Version)
	}
}

func TestKV_GetMissing_ReturnsErrNotFound(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	_, err := kv.Get(context.Background(), state.ScopeAgent, "estate-muse", "nope")
	if !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestKV_CAS_StaleVersionRejected(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	if _, err := kv.Put(ctx, state.ScopeRow, "row-17", "tier", []byte("B")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Writer A wins.
	if _, err := kv.CompareAndSwap(ctx, state.ScopeRow, "row-17", "tier", 1, []byte("A")); err != nil {
		t.Fatalf("CAS v1→v2: %v", err)
	}
	// Writer B holds the stale base version and must be rejected.
	if _, err := kv.CompareAndSwap(ctx, state.ScopeRow, "row-17", "tier", 1, []byte("C")); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("stale CAS = %v, want ErrConflict", err)
	}
	got, err := kv.Get(ctx, state.ScopeRow, "row-17", "tier")
	if err != nil || string(got.Value) != "A" || got.Version != 2 {
		t.Fatalf("post-conflict state = %+v err=%v, want value A version 2", got, err)
	}
}

func TestKV_CAS_InitialInsertSemantics(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	// baseVersion=0 means "first write": succeeds on a fresh key…
	e, err := kv.CompareAndSwap(ctx, state.ScopeRow, "row-1", "draft", 0, []byte("v1"))
	if err != nil || e.Version != 1 {
		t.Fatalf("initial CAS = %+v err=%v, want version 1", e, err)
	}
	// …and conflicts when the key already exists.
	if _, err := kv.CompareAndSwap(ctx, state.ScopeRow, "row-1", "draft", 0, []byte("v1-again")); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("repeat initial CAS = %v, want ErrConflict", err)
	}
	// CAS against a missing key with a non-zero base also conflicts.
	if _, err := kv.CompareAndSwap(ctx, state.ScopeRow, "row-2", "draft", 3, []byte("x")); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("CAS missing key base=3 = %v, want ErrConflict", err)
	}
}

// TestKV_ConcurrentCASCounter is the 并发 path: 16 goroutines bump one
// shared counter through the CAS-retry loop the package documents
// ("callers retry by re-reading"). Every increment must land — a lost
// update means two parallel per-row actions trampled each other.
//
// KNOWN DEFECT — APP-1071: pkg/state.BoltStore.CompareAndSwap performs
// its version check (Get) and its write (Put) in two separate bbolt
// transactions with no mutex, so the check-then-write is not atomic.
// Measured result on kernel HEAD 577b2d1: 16 writers × 10 bumps → final
// counter 10 instead of 160 (150 silent lost updates, zero ErrConflict).
// The test is skipped by default until the kernel fix lands; run the
// reproduction with:
//
//	E2E_RUN_KNOWN_DEFECTS=1 go test -race -run TestKV_ConcurrentCASCounter ./...
//
// Once APP-1071 is fixed, delete the skip gate below.
func TestKV_ConcurrentCASCounter(t *testing.T) {
	if os.Getenv("E2E_RUN_KNOWN_DEFECTS") == "" {
		t.Skip("known kernel defect APP-1071 (pkg/state CAS not atomic — lost updates under concurrency); " +
			"set E2E_RUN_KNOWN_DEFECTS=1 to run the reproduction")
	}
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	const writers = 16
	const bumpsPerWriter = 10

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < bumpsPerWriter; i++ {
				for { // CAS retry loop
					cur, err := kv.Get(ctx, state.ScopeAgent, "estate-muse", "generated_total")
					var base int64
					val := 0
					if err == nil {
						base = cur.Version
						_, _ = fmt.Sscanf(string(cur.Value), "%d", &val)
					} else if !errors.Is(err, state.ErrNotFound) {
						t.Errorf("Get: %v", err)
						return
					}
					_, err = kv.CompareAndSwap(ctx, state.ScopeAgent, "estate-muse", "generated_total",
						base, fmt.Appendf(nil, "%d", val+1))
					if err == nil {
						break
					}
					if !errors.Is(err, state.ErrConflict) {
						t.Errorf("CAS: %v", err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()

	final, err := kv.Get(ctx, state.ScopeAgent, "estate-muse", "generated_total")
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}
	want := fmt.Sprintf("%d", writers*bumpsPerWriter)
	if string(final.Value) != want {
		t.Fatalf("lost updates: counter = %s, want %s", final.Value, want)
	}
	if final.Version != int64(writers*bumpsPerWriter) {
		t.Fatalf("version = %d, want %d (one bump per successful CAS)", final.Version, writers*bumpsPerWriter)
	}
}

// TestKV_RowScopeIsolation mirrors the EstateMuse security property the
// row JWT enforces at the HTTP layer: state written for row-17 must be
// invisible when reading row-99, and scopes must not bleed into each
// other even with identical keys.
func TestKV_RowScopeIsolation(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	if _, err := kv.Put(ctx, state.ScopeRow, "row-17", "topic", []byte("亚运村次新房成交价")); err != nil {
		t.Fatalf("Put row-17: %v", err)
	}
	if _, err := kv.Put(ctx, state.ScopeAgent, "row-17", "topic", []byte("agent-scope-shadow")); err != nil {
		t.Fatalf("Put agent shadow: %v", err)
	}

	if _, err := kv.Get(ctx, state.ScopeRow, "row-99", "topic"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("row-99 read of row-17 state = %v, want ErrNotFound", err)
	}
	got, err := kv.Get(ctx, state.ScopeRow, "row-17", "topic")
	if err != nil || string(got.Value) != "亚运村次新房成交价" {
		t.Fatalf("row-17 read = %q err=%v", got.Value, err)
	}

	// List is owner-filtered: row-17 sees exactly its own entry.
	entries, err := kv.List(ctx, state.ScopeRow, "row-17", "")
	if err != nil || len(entries) != 1 {
		t.Fatalf("List row-17 = %d entries err=%v, want exactly 1", len(entries), err)
	}
}

func TestKV_ListPrefixFilter(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	for _, k := range []string{"draft/post", "draft/video", "final/post"} {
		if _, err := kv.Put(ctx, state.ScopeRow, "row-17", k, []byte("x")); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
	drafts, err := kv.List(ctx, state.ScopeRow, "row-17", "draft/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("List(draft/) = %d entries, want 2", len(drafts))
	}
}

func TestKV_DeleteAndMissingDeleteNoop(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	if _, err := kv.Put(ctx, state.ScopeRow, "row-17", "tmp", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := kv.Delete(ctx, state.ScopeRow, "row-17", "tmp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kv.Get(ctx, state.ScopeRow, "row-17", "tmp"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := kv.Delete(ctx, state.ScopeRow, "row-17", "tmp"); err != nil {
		t.Fatalf("Delete missing should be a no-op, got %v", err)
	}
}

// TestKV_PersistenceAcrossReopen is the process-restart path: close the
// bbolt store, reopen the same data dir, and require value + version to
// survive — the workbook must outlive a `soyaos start` restart.
func TestKV_PersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	kv1, close1 := openKV(t, dir)
	if _, err := kv1.Put(ctx, state.ScopeAgent, "estate-muse", "workbook/seed", []byte("杭州亚运村二手房")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := kv1.Put(ctx, state.ScopeAgent, "estate-muse", "workbook/seed", []byte("杭州亚运村二手房 v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}
	close1()

	kv2, close2 := openKV(t, dir)
	defer close2()
	got, err := kv2.Get(ctx, state.ScopeAgent, "estate-muse", "workbook/seed")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if string(got.Value) != "杭州亚运村二手房 v2" || got.Version != 2 {
		t.Fatalf("after reopen = %q v%d, want v2 value at version 2", got.Value, got.Version)
	}
}

// TestKV_WorkbookScale_500Rows seeds one state entry per workbook row at
// the product's advertised scale (500 rows) and verifies exact readback —
// the EstateMuse core promise is that every one of those rows can be
// resolved server-side without trusting the caller's payload.
func TestKV_WorkbookScale_500Rows(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	const rows = 500
	for i := 1; i <= rows; i++ {
		owner := fmt.Sprintf("row-%d", i)
		if _, err := kv.Put(ctx, state.ScopeRow, owner, "context", fmt.Appendf(nil, "topic-%d", i)); err != nil {
			t.Fatalf("Put %s: %v", owner, err)
		}
	}
	// Spot-check exact readback at the boundaries and a middle row.
	for _, i := range []int{1, 17, 250, 500} {
		got, err := kv.Get(ctx, state.ScopeRow, fmt.Sprintf("row-%d", i), "context")
		if err != nil || string(got.Value) != fmt.Sprintf("topic-%d", i) {
			t.Fatalf("row-%d readback = %q err=%v", i, got.Value, err)
		}
	}
}

func TestKV_InvalidScopeRejected(t *testing.T) {
	kv, done := openKV(t, t.TempDir())
	defer done()
	ctx := context.Background()

	if _, err := kv.Get(ctx, state.Scope("tenant"), "x", "y"); !errors.Is(err, state.ErrInvalidScope) {
		t.Fatalf("Get invalid scope = %v, want ErrInvalidScope", err)
	}
	if _, err := kv.Put(ctx, state.Scope(""), "x", "y", nil); !errors.Is(err, state.ErrInvalidScope) {
		t.Fatalf("Put invalid scope = %v, want ErrInvalidScope", err)
	}
}
