// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	catalog "github.com/dsx-ai-factory/run-ai/runai-clients/catalog-service-client/generated"
)

func TestRecommendationHTTPContract(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/inference/right-sizing/recommendation" {
					t.Errorf("wrong route: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer <test-token>" {
					t.Error("missing authentication")
				}
				var request map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["image"] != "nvcr.io/nim/nvidia/example" || request["tag"] != "1.2.3" || request["clusterId"] != "cluster-id" || request["gpuType"] != "A100" {
					t.Errorf("wrong request: %+v", request)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status != http.StatusOK {
					_, _ = w.Write([]byte(`{"code":503,"message":"NGC sizing metadata unavailable"}`))
					return
				}
				_, _ = w.Write([]byte(`{"image":"nvcr.io/nim/nvidia/example","tag":"1.2.3","results":[{"gpuType":"A100","status":"recommended","minimumGpuMemoryPerDeviceGb":36,"recommendation":{"allocationType":"gpu_memory","gpuCount":1,"gpuMemoryPerDeviceBytes":36000000000,"estimated":false}},{"gpuType":"T4","status":"no_recommendation","reason":"Missing capacity"}]}`))
			}))
			defer server.Close()
			request := catalog.NewRightSizingRequest("cluster-id", "nvcr.io/nim/nvidia/example", "1.2.3")
			request.SetGpuType("A100")
			var output bytes.Buffer
			err := runRecommendation(context.Background(), server.URL, "<test-token>", *request, &output)
			if status != http.StatusOK {
				if err == nil || !strings.Contains(err.Error(), "NGC sizing metadata unavailable") || output.Len() != 0 {
					t.Fatalf("upstream failure hidden: %v %s", err, output.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Results []map[string]interface{} `json:"results"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Results) != 2 || result.Results[0]["minimumGpuMemoryPerDeviceGb"] != float64(36) {
				t.Fatalf("bad output: %s", output.String())
			}
			recommendation := result.Results[0]["recommendation"].(map[string]interface{})
			if recommendation["estimated"] != false {
				t.Fatalf("lost false estimated: %s", output.String())
			}
			if result.Results[1]["recommendation"] != nil {
				t.Fatalf("invented recommendation: %s", output.String())
			}
		})
	}
}
