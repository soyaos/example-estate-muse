// manifest_test.go verifies that the real soyapack.yaml shipped in this
// repo (a) passes the authoritative pkg/soyapack validator and (b)
// actually declares the state / action / auth surfaces the rest of the
// e2e suite exercises. If someone edits the manifest in a way that
// breaks the EstateMuse runtime contract, this is the first test to go
// red.
package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/soyaos/soyaos/pkg/soyapack"
)

// packDir returns the absolute path of the pack root (the directory
// containing soyapack.yaml), i.e. the parent of this e2e module.
func packDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve pack dir: %v", err)
	}
	return abs
}

func loadManifest(t *testing.T) *soyapack.Manifest {
	t.Helper()
	m, err := soyapack.LoadFromFile(filepath.Join(packDir(t), "soyapack.yaml"))
	if err != nil {
		t.Fatalf("load soyapack.yaml: %v", err)
	}
	return m
}

func TestManifest_PassesAuthoritativeValidator(t *testing.T) {
	m := loadManifest(t)
	if err := soyapack.Validate(m); err != nil {
		t.Fatalf("soyapack.Validate rejected the shipped manifest: %v", err)
	}
}

func TestManifest_DeclaresKVAgentState(t *testing.T) {
	m := loadManifest(t)
	if m.State == nil {
		t.Fatal("manifest.state missing — EstateMuse is a Stateful Agent (DD-010)")
	}
	if m.State.Scope != "agent" {
		t.Fatalf("state.scope = %q, want \"agent\"", m.State.Scope)
	}
	if m.State.Store != "kv" {
		t.Fatalf("state.store = %q, want \"kv\"", m.State.Store)
	}
}

func TestManifest_DeclaresPerRowActionsWithExistingHandlers(t *testing.T) {
	m := loadManifest(t)
	want := map[string]bool{"generate_post": false, "generate_video": false}
	for _, a := range m.Actions {
		if _, ok := want[a.ID]; !ok {
			continue
		}
		want[a.ID] = true
		if a.On != "per_row" {
			t.Errorf("action %q: on = %q, want \"per_row\"", a.ID, a.On)
		}
		handlerPath := filepath.Join(packDir(t), a.Handler)
		if _, err := os.Stat(handlerPath); err != nil {
			t.Errorf("action %q: handler prompt missing at %s: %v", a.ID, handlerPath, err)
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("manifest is missing the %q per-row action", id)
		}
	}
}

func TestManifest_ExposesVirtualModelID(t *testing.T) {
	m := loadManifest(t)
	if m.Expose == nil || m.Expose.VirtualModelID != "soya:estate-muse" {
		t.Fatalf("expose.virtual_model_id missing or wrong: %+v", m.Expose)
	}
}
