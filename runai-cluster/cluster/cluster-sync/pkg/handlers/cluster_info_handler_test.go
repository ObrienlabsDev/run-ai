// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	swagger "github.com/dsx-ai-factory/run-ai/control-plane/cluster-service/pkg/client/generated"
	"github.com/stretchr/testify/require"
)

func TestUpdateClusterInfoMountCheckCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		message   string
		capable   bool
		wantCalls int
		wantError bool
	}{
		{name: "current control plane", status: http.StatusNoContent, capable: true, wantCalls: 1},
		{name: "older control plane", status: http.StatusBadRequest, message: `json: unknown field "s3FileSystemMountCheckImage"`, capable: true, wantCalls: 2},
		{name: "unrelated unknown field", status: http.StatusBadRequest, message: `json: unknown field "other"`, capable: true, wantCalls: 1, wantError: true},
		{name: "server failure", status: http.StatusInternalServerError, capable: true, wantCalls: 1, wantError: true},
		{name: "capability already absent", status: http.StatusBadRequest, message: `json: unknown field "s3FileSystemMountCheckImage"`, wantCalls: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var capabilities []bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body swagger.ClusterInfoSyncRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode cluster report: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				reportedStatus := body.GetStatus()
				config := reportedStatus.GetConfig()
				if config.GetS3FileSystemImage() != "example.com/goofys:current" {
					t.Error("compatibility retry lost the configured filesystem image")
				}
				capabilities = append(capabilities, config.HasS3FileSystemMountCheckImage())
				if len(capabilities) > 1 {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if tc.status != http.StatusNoContent {
					if err := json.NewEncoder(w).Encode(swagger.Error{Code: int32(tc.status), Message: tc.message}); err != nil {
						t.Errorf("encode error response: %v", err)
					}
				}
			}))
			defer server.Close()
			clientConfig := swagger.NewConfiguration()
			clientConfig.Servers = swagger.ServerConfigurations{{URL: server.URL}}
			clientConfig.HTTPClient = server.Client()
			config := swagger.ClusterReportedConfig{}
			config.SetS3FileSystemImage("example.com/goofys:current")
			if tc.capable {
				config.SetS3FileSystemMountCheckImage(config.GetS3FileSystemImage())
			}
			status := swagger.ClusterReportedStatus{}
			status.SetConfig(config)
			info := swagger.ClusterInfoSyncRequest{Domain: "cluster.example.com", Version: "current"}
			info.SetStatus(status)
			res, err := updateClusterInfo(context.Background(), swagger.NewAPIClient(clientConfig), "cluster-id", info)
			if res != nil {
				require.NoError(t, res.Body.Close())
			}
			require.Equal(t, tc.wantError, err != nil)
			require.Len(t, capabilities, tc.wantCalls)
			require.Equal(t, tc.capable, capabilities[0])
			if tc.wantCalls == 2 {
				require.False(t, capabilities[1], "the legacy report must omit the unsupported field")
			}
		})
	}
}
