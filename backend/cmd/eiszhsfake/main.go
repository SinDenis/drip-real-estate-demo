// Command eiszhsfake serves deterministic offline ЕИСЖС fixtures for E2E tests.
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

//nolint:funlen,gocognit,gocyclo // One deterministic test server keeps fixtures easy to audit.
func main() {
	var failed atomic.Bool
	var currentRequests atomic.Int64
	var currentInFlight atomic.Int64
	var currentBlocked atomic.Bool
	var currentMode atomic.Int32
	router := http.NewServeMux()
	router.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("POST /token", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, map[string]any{
			"access_token": "offline-e2e-token", "token_type": "Bearer", "expires_in": 300,
		})
	})
	router.HandleFunc("POST /admin/mode", func(response http.ResponseWriter, request *http.Request) {
		failed.Store(request.URL.Query().Get("value") == "failed")
		response.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("GET /admin/stats", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, map[string]any{
			"currentRequests": currentRequests.Load(), "currentInFlight": currentInFlight.Load(),
		})
	})
	router.HandleFunc("POST /admin/current-block", func(response http.ResponseWriter, request *http.Request) {
		currentBlocked.Store(request.URL.Query().Get("value") == "blocked")
		response.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("POST /admin/current-mode", func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("value") {
		case "incomplete":
			currentMode.Store(1)
		case "overlap":
			currentMode.Store(2)
		case "paged-duplicates":
			currentMode.Store(3)
		default:
			currentMode.Store(0)
		}
		response.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("GET /complexes", func(response http.ResponseWriter, request *http.Request) {
		if failed.Load() {
			http.Error(response, "offline source unavailable", http.StatusServiceUnavailable)
			return
		}
		region := request.URL.Query().Get("region-code")
		writeJSON(response, map[string]any{
			"total": 2, "limitItems": 50,
			"elements": map[string]any{"complexShortInfo": []map[string]any{
				{"complexId": 1001, "fullName": "ЖК Северный, регион " + region, "developerId": 1},
				{"complexId": 1002, "fullName": "ЖК Центральный, регион " + region, "developerId": 1},
			}},
		})
	})
	router.HandleFunc("GET /complexes/{complexID}/construction-objects", func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		currentRequests.Add(1)
		currentInFlight.Add(1)
		defer currentInFlight.Add(-1)
		if !waitUntilCurrentReleased(request, &currentBlocked) {
			return
		}
		if failed.Load() {
			http.Error(response, "offline source unavailable", http.StatusServiceUnavailable)
			return
		}
		complexID, err := strconv.ParseInt(request.PathValue("complexID"), 10, 64)
		if err != nil {
			http.Error(response, "invalid complex", http.StatusBadRequest)
			return
		}
		page := request.URL.Query().Get("page")
		pagedDuplicates := currentMode.Load() == 3
		pageAllowed := page == "0" ||
			(currentMode.Load() == 2 && page == "1") ||
			(pagedDuplicates && complexID == 1001 && page == "1")
		if !pageAllowed || request.URL.Query().Get("size") != "50" {
			http.Error(response, "unexpected pagination", http.StatusBadRequest)
			return
		}
		if pagedDuplicates {
			writePagedDuplicateMembership(response, complexID, page)
			return
		}
		total := 1
		if currentMode.Load() == 2 {
			total = 2
		}
		writeJSON(response, map[string]any{
			"total": total, "limitItems": 50,
			"elements": map[string]any{"constructionObjectShortInfo": []map[string]any{
				{"objectId": complexID + 1000, "complexId": complexID},
			}},
		})
	})
	router.HandleFunc("GET /construction-objects/{objectID}/extended", func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		currentRequests.Add(1)
		if failed.Load() {
			http.Error(response, "offline source unavailable", http.StatusServiceUnavailable)
			return
		}
		objectID, err := strconv.ParseInt(request.PathValue("objectID"), 10, 64)
		if err != nil {
			http.Error(response, "invalid object", http.StatusBadRequest)
			return
		}
		if currentMode.Load() == 1 {
			writeJSON(response, map[string]any{
				"objectId": objectID,
				"dduInfo":  map[string]any{"dduLivNumber": 17},
			})
			return
		}
		writeJSON(response, map[string]any{
			"objectId": objectID,
			"dduInfo": map[string]any{
				"dduLivNumber": 17, "dduNonlivNumber": 4, "dduParkingNumber": 2,
			},
		})
	})
	server := &http.Server{Addr: ":8090", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
}

func writePagedDuplicateMembership(response http.ResponseWriter, complexID int64, page string) {
	objects := make([]map[string]any, 0, 50)
	total := 2
	if complexID == 1001 {
		total = 51
		if page == "0" {
			for range 50 {
				objects = append(objects, map[string]any{"objectId": 9001, "complexId": complexID})
			}
		} else {
			objects = append(objects, map[string]any{"objectId": 9002, "complexId": complexID})
		}
	} else {
		objects = append(objects,
			map[string]any{"objectId": 9001, "complexId": complexID},
			map[string]any{"objectId": 9003, "complexId": complexID},
		)
	}
	writeJSON(response, map[string]any{
		"total": total, "limitItems": 50,
		"elements": map[string]any{"constructionObjectShortInfo": objects},
	})
}

func waitUntilCurrentReleased(request *http.Request, blocked *atomic.Bool) bool {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for blocked.Load() {
		select {
		case <-request.Context().Done():
			return false
		case <-ticker.C:
		}
	}
	return true
}

func writeJSON(response http.ResponseWriter, body any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(body)
}
