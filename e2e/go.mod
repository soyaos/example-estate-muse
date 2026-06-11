// E2E verification module for the EstateMuse SoyaPack (APP-1059).
//
// This module is intentionally separate from the pack itself — the pack
// stays a pure declarative SoyaPack (yaml + prompts + templates), while
// this module imports the SoyaOS kernel packages to verify, end to end,
// the two runtime capabilities the manifest leans on:
//
//   - state: { scope: agent, store: kv }  → pkg/state KV store
//   - per-row JWT auth on action buttons  → pkg/auth/rowtoken + gateway
//
// The kernel is resolved from the sibling checkout (../../soyaos) because
// github.com/soyaos/soyaos is not yet published as a versioned module.
module github.com/soyaos/example-estate-muse/e2e

go 1.23.0

require github.com/soyaos/soyaos v0.0.0

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	golang.org/x/sys v0.33.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/soyaos/soyaos => ../../soyaos
