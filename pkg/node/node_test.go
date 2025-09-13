package node

import (
	"reflect"
	"testing"

	"github.com/mchmarny/gpuid/pkg/logger"
)

func TestParseNodeInfo(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		want       *Info
		wantErr    bool
	}{
		{
			name:       "valid AWS providerID",
			providerID: "aws:///us-west-2a/i-0123456789abcdef0",
			want: &Info{
				Provider:   "aws",
				Identifier: "i-0123456789abcdef0",
				Raw:        "aws:///us-west-2a/i-0123456789abcdef0",
			},
			wantErr: false,
		},
		{
			name:       "valid GCP providerID",
			providerID: "gce://my-gcp-project/us-central1-a/gke-cluster-default-pool-12345678-abcd",
			want: &Info{
				Provider:   "gce",
				Identifier: "gke-cluster-default-pool-12345678-abcd",
				Raw:        "gce://my-gcp-project/us-central1-a/gke-cluster-default-pool-12345678-abcd",
			},
			wantErr: false,
		},
		{
			name:       "valid Azure providerID",
			providerID: "azure:///subscriptions/sub-id/resourceGroups/rg-name/providers/Microsoft.Compute/virtualMachines/vm-name",
			want: &Info{
				Provider:   "azure",
				Identifier: "vm-name",
				Raw:        "azure:///subscriptions/sub-id/resourceGroups/rg-name/providers/Microsoft.Compute/virtualMachines/vm-name",
			},
			wantErr: false,
		},
		{
			name:       "valid Kind providerID",
			providerID: "kind://docker/gpuid/gpuid-worker",
			want: &Info{
				Provider:   "kind",
				Identifier: "gpuid-worker",
				Raw:        "kind://docker/gpuid/gpuid-worker",
			},
			wantErr: false,
		},
		{
			name:       "invalid providerID format",
			providerID: "invalid-format",
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "empty providerID",
			providerID: "",
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNodeInfo(logger.NewTestLogger(t), tt.providerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNodeInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}
