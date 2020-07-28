/*
Copyright 2019,2020 Intel Corporation

SPDX-License-Identifier: Apache-2.0
*/

package parameters

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/stretchr/testify/assert"
)

func TestParameters(t *testing.T) {
	no := false
	foo := "foo"
	gig := "1Gi"
	gigNum := int64(1 * 1024 * 1024 * 1024)
	name := "joe"

	tests := []struct {
		name       string
		origin     Origin
		stringmap  map[string]string
		parameters Volume
		err        string
	}{
		{
			name:   "createvolume",
			origin: CreateVolumeOrigin,
			stringmap: VolumeContext{
				EraseAfter: "false",
			},
			parameters: Volume{
				EraseAfter: &no,
			},
		},
		{
			name:   "bad-volumeid",
			origin: CreateVolumeOrigin,
			stringmap: VolumeContext{
				VolumeID: foo,
			},
			err: `parameter "_id" invalid in this context`,
		},
		{
			name:   "good-volumeid",
			origin: CreateVolumeInternalOrigin,
			stringmap: VolumeContext{
				VolumeID: "foo",
			},
			parameters: Volume{
				VolumeID: &foo,
			},
		},
		{
			name:   "createvolumeinternal",
			origin: CreateVolumeInternalOrigin,
			stringmap: VolumeContext{
				EraseAfter: "false",
				VolumeID:   "foo",
			},
			parameters: Volume{
				EraseAfter: &no,
				VolumeID:   &foo,
			},
		},
		{
			name:   "publishpersistent",
			origin: PersistentVolumeOrigin,
			stringmap: VolumeContext{
				EraseAfter: "false",

				Name:                     name,
				"csi.storage.k8s.io/foo": "bar",
				ProvisionerID:            "provisioner XYZ",
			},
			parameters: Volume{
				EraseAfter: &no,
				Name:       &name,
			},
		},
		{
			name:   "node",
			origin: NodeVolumeOrigin,
			stringmap: VolumeContext{
				EraseAfter: "false",
				Size:       gig,
				Name:       name,
			},
			parameters: Volume{
				EraseAfter: &no,
				Size:       &gigNum,
				Name:       &name,
			},
		},

		// Various parameters which are not allowed in this context.
		{
			name:   "invalid-parameter-create",
			origin: CreateVolumeOrigin,
			stringmap: VolumeContext{
				VolumeID: "volume-id-chosen-by-attacker",
			},
			err: "parameter \"_id\" invalid in this context",
		},
		{
			name:   "invalid-node-context",
			origin: NodeVolumeOrigin,
			stringmap: VolumeContext{
				VolumeID: "volume-id",
			},
			err: "parameter \"_id\" invalid in this context",
		},

		// Parse errors for size.
		{
			name:   "invalid-size-suffix",
			origin: PersistentVolumeOrigin,
			stringmap: VolumeContext{
				Size: "1X",
			},
			err: "parameter \"size\": failed to parse \"1X\" as int64: quantities must match the regular expression '^([+-]?[0-9.]+)([eEinumkKMGTP]*[-+]?[0-9]*)$'",
		},
		{
			name:   "invalid-size-string",
			origin: PersistentVolumeOrigin,
			stringmap: VolumeContext{
				Size: "foo",
			},
			err: "parameter \"size\": failed to parse \"foo\" as int64: quantities must match the regular expression '^([+-]?[0-9.]+)([eEinumkKMGTP]*[-+]?[0-9]*)$'",
		},
	}
	for _, tt := range tests {
		tt := tt
		filteredMap := func() VolumeContext {
			result := VolumeContext{}
			for key, value := range tt.stringmap {
				switch key {
				case Size:
					quantity := resource.MustParse(value)
					value = fmt.Sprintf("%d", quantity.Value())
				}
				if key != VolumeID &&
					key != ProvisionerID &&
					!strings.HasPrefix(key, PodInfoPrefix) {
					result[key] = value
				}
			}
			return result
		}

		t.Run(tt.name, func(t *testing.T) {
			parameters, err := Parse(tt.origin, tt.stringmap)
			switch {
			case tt.err == "":
				if assert.NoError(t, err, "no parse error") &&
					assert.Equal(t, tt.parameters, parameters) {
					stringmap := parameters.ToContext()
					assert.Equal(t, filteredMap(), stringmap, "re-encoded volume context")
				}
			case err == nil:
				assert.Error(t, err, "expected error: "+tt.err)
			default:
				assert.Equal(t, tt.err, err.Error(), "parse error")
			}
		})
	}
}
