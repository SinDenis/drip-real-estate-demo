package httpapi_test

import (
	"encoding/csv"
	"mime"
	"reflect"
	"strings"
	"testing"
)

func TestSeminarExport(t *testing.T) {
	for _, mode := range []string{"known", "zero", "unknown", "no_dashboard"} {
		t.Run(mode, func(t *testing.T) {
			store, h := ceFixture()
			expected := "175"
			switch mode {
			case "zero":
				*store.source.CurrentDashboard.CumulativeDDUCount = 0
				expected = "0"
			case "unknown":
				store.source.CurrentDashboard.CumulativeDDUCount = nil
				expected = ""
			case "no_dashboard":
				store.source.CurrentDashboard = nil
				expected = ""
			}
			original := store.source
			r := ceRequest(h, "GET", "/api/v1/sessions/"+ceSourceID+"/export.csv", "active", "", "")
			if r.Code != 200 {
				t.Fatalf("status=%d body=%s", r.Code, r.Body)
			}
			content, params, err := mime.ParseMediaType(r.Header().Get("Content-Type"))
			if err != nil || content != "text/csv" || strings.ToLower(params["charset"]) != "utf-8" {
				t.Fatalf("content type=%s", r.Header().Get("Content-Type"))
			}
			disposition, params, err := mime.ParseMediaType(r.Header().Get("Content-Disposition"))
			if err != nil || disposition != "attachment" || params["filename"] != "session-"+ceSourceID+".csv" {
				t.Fatalf("disposition=%s", r.Header().Get("Content-Disposition"))
			}
			rows, err := csv.NewReader(strings.NewReader(r.Body.String())).ReadAll()
			if err != nil || len(rows) != 2 {
				t.Fatalf("invalid CSV: %v %s", err, r.Body)
			}
			if !reflect.DeepEqual(rows[0], []string{"session_id", "name", "status", "source_label", "cumulative_ddu_count"}) {
				t.Fatalf("header=%v", rows[0])
			}
			if len(rows[1]) != 5 {
				t.Fatalf("record=%v", rows[1])
			}
			if rows[1][0] != ceSourceID || rows[1][1] != store.source.Name || rows[1][2] != "complete" || rows[1][4] != expected {
				t.Fatalf("record=%v expectedCount=%q", rows[1], expected)
			}
			if mode != "no_dashboard" && rows[1][3] != "Текущий срез ЕИСЖС" {
				t.Fatalf("source=%q", rows[1][3])
			}
			if mode == "no_dashboard" && rows[1][3] != "" {
				t.Fatalf("invented source=%q", rows[1][3])
			}
			if !reflect.DeepEqual(original, store.source) || len(store.created) != 0 {
				t.Fatal("export changed stored session")
			}
		})
	}
}
func TestSeminarExportErrors(t *testing.T) {
	for _, test := range []struct {
		name, cookie string
		status       int
	}{
		{"anonymous", "", 401}, {"pending", "pending", 401}, {"missing", "active", 404}, {"storage", "active", 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, h := ceFixture()
			store.missing = test.name == "missing"
			store.failure = test.name == "storage"
			ceError(t, ceRequest(h, "GET", "/api/v1/sessions/"+ceSourceID+"/export.csv", test.cookie, "", ""), test.status)
		})
	}
}
