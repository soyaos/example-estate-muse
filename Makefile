# example-estate-muse — verification entry points (APP-1059)
#
# The pack itself is declarative (yaml + prompts + templates); the e2e/
# module verifies the two runtime capabilities the manifest leans on
# against the sibling SoyaOS kernel checkout (../soyaos):
#
#   make e2e    KV state-store verification + row-scoped JWT end-to-end
#               auth tests (positive + negative / 越权 cases), run with
#               -race against real bbolt files and a real HTTP gateway.
#   make test   alias for e2e.

.PHONY: test e2e

test: e2e

e2e:
	cd e2e && go test -race -count=1 -v ./...
