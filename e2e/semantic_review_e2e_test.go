package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestE2E_RowActionRejectsFailedSemanticReview(t *testing.T) {
	h := startHarness(t, `{"approved":false,"findings":[{"quote":"MOCK","reason":"test rejection"}],"checks":[{"id":"business_context","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"factual_claims","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"method_logic","passed":false,"reason":"Protocol fixture; not factual evaluation"},{"id":"format","passed":true,"reason":"Protocol fixture; not factual evaluation"},{"id":"safety","passed":true,"reason":"Protocol fixture; not factual evaluation"}]}`)
	token := h.mint(t, agentSlug, actionPost, "row-17", time.Hour)
	status, body := h.postAction(t, token, agentSlug, actionPost, "row-17")
	if status != http.StatusInternalServerError || body["output"] != nil {
		t.Fatalf("rejected draft delivered: status=%d body=%v", status, body)
	}
	if h.upstream.count() != 5 {
		t.Fatalf("want exactly 1 plan + 2 drafts + 2 reviews, got %d", h.upstream.count())
	}
}
