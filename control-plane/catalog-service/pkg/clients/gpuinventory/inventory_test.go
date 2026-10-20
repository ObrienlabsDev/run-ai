/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package gpuinventory

import (
	"context"
	"encoding/json"
	"errors"
	clusterclient "github.com/dsx-ai-factory/run-ai/control-plane/cluster-service/pkg/client"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	clusterapi "github.com/dsx-ai-factory/run-ai/control-plane/cluster-service/pkg/client/generated"
)

type nodePoolsFunc func(context.Context, string) (*clusterapi.GetNodepoolsMinimal200Response, error)

func (f nodePoolsFunc) GetNodePoolsMinimalByCluster(ctx context.Context, id string) (*clusterapi.GetNodepoolsMinimal200Response, error) {
	return f(ctx, id)
}

const clusterID = "4b94efde-b05a-4118-a03a-1dedff81fa8e"

func TestInventoryKeepsDistinctCapacitiesWithoutClaimingAvailability(t *testing.T) {
	small, large := int64(40_000_000_000), int64(80_000_000_000)
	client := New(nodePoolsFunc(func(_ context.Context, id string) (*clusterapi.GetNodepoolsMinimal200Response, error) {
		if id != clusterID {
			t.Fatalf("wrong cluster: %s", id)
		}
		return &clusterapi.GetNodepoolsMinimal200Response{Nodepools: []clusterapi.NodepoolMinimal{
			{GpuTypes: []clusterapi.NodepoolGpuTypeDetails{
				{Type: "A100", GpuMemoryBytes: &large, TotalDevices: 8},
				{Type: "A100", GpuMemoryBytes: &small, TotalDevices: 1},
			}},
			{GpuTypes: []clusterapi.NodepoolGpuTypeDetails{
				{Type: "A100", GpuMemoryBytes: &large, TotalDevices: 4},
				{Type: "T4", TotalDevices: 1},
			}},
		}}, nil
	}))
	got, err := client.Get(context.Background(), clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Type != "A100" || got[0].MemoryBytes == nil || *got[0].MemoryBytes != small ||
		got[1].MemoryBytes == nil || *got[1].MemoryBytes != large || got[2].Type != "T4" || got[2].MemoryBytes != nil {
		t.Fatalf("inventory lost a capacity or invented memory: %+v", got)
	}
}

func TestInventoryFailureIsNotAnEmptyCluster(t *testing.T) {
	upstream := errors.New("cluster service unavailable")
	client := New(nodePoolsFunc(func(context.Context, string) (*clusterapi.GetNodepoolsMinimal200Response, error) {
		return nil, upstream
	}))
	if _, err := client.Get(context.Background(), clusterID); !errors.Is(err, upstream) {
		t.Fatalf("upstream failure lost: %v", err)
	}
	client = New(nodePoolsFunc(func(context.Context, string) (*clusterapi.GetNodepoolsMinimal200Response, error) {
		return nil, nil
	}))
	if _, err := client.Get(context.Background(), clusterID); err == nil {
		t.Fatal("nil upstream response was accepted as an empty cluster")
	}
}

func TestInventoryValidatesClusterBeforeBuildingFilter(t *testing.T) {
	client := New(nodePoolsFunc(func(context.Context, string) (*clusterapi.GetNodepoolsMinimal200Response, error) {
		t.Fatal("invalid cluster reached inventory")
		return nil, nil
	}))
	for _, id := range []string{"", "cluster-id,tenantId==other"} {
		if _, err := client.Get(context.Background(), id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}

func TestInventoryReadsEveryNodepoolPage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		total      int
		failSecond bool
	}{
		{"GPU on second page", 51, false}, {"exact full page", 50, false}, {"empty cluster", 0, false}, {"second page fails", 51, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("filterBy") != "clusterId=="+clusterID {
					t.Errorf("wrong cluster filter: %s", r.URL.RawQuery)
				}
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
				if limit == 0 {
					limit = 50
				}
				if offset > 0 && tc.failSecond {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				pools := []clusterapi.NodepoolMinimal{}
				for i := offset; i < tc.total && i < offset+limit; i++ {
					pool := clusterapi.NodepoolMinimal{}
					if i == tc.total-1 {
						memory := int64(80_000_000_000)
						pool.GpuTypes = []clusterapi.NodepoolGpuTypeDetails{{Type: "A100", GpuMemoryBytes: &memory, TotalDevices: 1}}
					}
					pools = append(pools, pool)
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(clusterapi.GetNodepoolsMinimal200Response{Nodepools: pools}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			hardware, err := New(clusterclient.NewClusterClient(server.URL)).Get(context.Background(), clusterID)
			if tc.failSecond {
				if err == nil || hardware != nil {
					t.Fatalf("partial inventory returned: %+v %v", hardware, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.total > 0 && (len(hardware) != 1 || hardware[0].Type != "A100") {
				t.Fatalf("lost GPU from last nodepool: %+v", hardware)
			}
			if tc.total == 50 && calls != 2 {
				t.Fatalf("full page must be followed by empty page: calls=%d", calls)
			}
			if tc.total == 0 && (len(hardware) != 0 || calls != 1) {
				t.Fatalf("empty inventory: %+v calls=%d", hardware, calls)
			}
		})
	}
}
