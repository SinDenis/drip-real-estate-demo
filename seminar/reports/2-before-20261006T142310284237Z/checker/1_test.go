package httpapi_test

import "testing"

func TestSeminarLive(t *testing.T) {
	_, h := ceFixture()
	r := ceRequest(h, "GET", "/health/live", "", "", "")
	if r.Code != 200 {
		t.Fatalf("liveness=%d", r.Code)
	}
	body := ceJSON(t, r)
	if body["status"] != "live" || body["service"] != "DRIP Недвижимость" {
		t.Fatalf("liveness=%v", body)
	}
	r = ceRequest(h, "GET", "/health/ready", "", "", "")
	ready := ceJSON(t, r)
	if r.Code != 503 || ready["status"] != "not_ready" || len(ready) != 1 {
		t.Fatalf("readiness changed: %d %s", r.Code, r.Body)
	}
}
