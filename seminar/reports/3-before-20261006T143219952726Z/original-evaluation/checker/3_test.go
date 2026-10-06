package httpapi_test

import (
	"reflect"
	"testing"
)

func TestSeminarDuplicate(t *testing.T) {
	store, h := ceFixture()
	original := store.source
	path := "/api/v1/sessions/" + ceSourceID + "/duplicate"
	first := ceRequest(h, "POST", path, "active", "duplicate-1", "")
	if first.Code != 201 {
		t.Fatalf("status=%d body=%s", first.Code, first.Body)
	}
	body := ceJSON(t, first)
	id, ok := body["id"].(string)
	if !ok || id == "" || id == ceSourceID {
		t.Fatalf("new ID=%v", body["id"])
	}
	if first.Header().Get("Location") != "/api/v1/sessions/"+id {
		t.Fatalf("Location=%q", first.Header().Get("Location"))
	}
	if len(store.created) != 1 {
		t.Fatalf("created=%d", len(store.created))
	}
	saved := store.created[0].Session
	if !reflect.DeepEqual(saved.RegionCodes, original.RegionCodes) || !reflect.DeepEqual(saved.ResidentialComplexIDs, original.ResidentialComplexIDs) || !reflect.DeepEqual(saved.SegmentCodes, original.SegmentCodes) {
		t.Fatalf("filters changed: %+v", saved)
	}
	if saved.CreatedByID != ceEmployeeID || !saved.CreatedAt.Equal(ceNow) || saved.Status != "collecting" || saved.Name == original.Name {
		t.Fatalf("ordinary creation rules not applied: %+v", saved)
	}
	if saved.CurrentDashboard != nil || len(saved.ObservationIDs) != 0 {
		t.Fatalf("copied stale results: %+v", saved)
	}
	if !reflect.DeepEqual(original, store.source) {
		t.Fatal("source mutated")
	}
	second := ceRequest(h, "POST", path, "active", "duplicate-1", "")
	if second.Code != 201 || ceJSON(t, second)["id"] != id || len(store.created) != 1 {
		t.Fatalf("replay failed: %d %s creates=%d", second.Code, second.Body, len(store.created))
	}
	store.source.ResidentialComplexIDs = []string{"1002"}
	ceError(t, ceRequest(h, "POST", path, "active", "duplicate-1", ""), 409)
}
func TestSeminarDuplicateErrors(t *testing.T) {
	for _, test := range []struct {
		name, cookie, key string
		status            int
	}{
		{"anonymous", "", "key", 401}, {"pending", "pending", "key", 401},
		{"missing", "active", "key", 404}, {"storage", "active", "key", 500},
		{"missing_key", "active", "", 400}, {"invalid_key", "active", "   ", 422},
		{"invalid_filters", "active", "key", 422},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, h := ceFixture()
			store.missing = test.name == "missing"
			store.failure = test.name == "storage"
			if test.name == "invalid_filters" {
				store.source.RegionCodes = nil
			}
			ceError(t, ceRequest(h, "POST", "/api/v1/sessions/"+ceSourceID+"/duplicate", test.cookie, test.key, ""), test.status)
			if len(store.created) != 0 {
				t.Fatal("invalid request wrote a new session")
			}
		})
	}
}
