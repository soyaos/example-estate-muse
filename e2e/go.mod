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

require (
	github.com/soyaos/soyaos v0.0.0
	github.com/xuri/excelize/v2 v2.9.1
)

require (
	github.com/chromedp/cdproto v0.0.0-20241022234722-4d5d5faf59fb // indirect
	github.com/chromedp/chromedp v0.11.2 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/richardlehane/mscfb v1.0.6 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/soyaos/soyaos => ../../soyaos
