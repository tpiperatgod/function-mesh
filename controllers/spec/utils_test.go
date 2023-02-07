// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	computeapi "github.com/streamnative/function-mesh/api/compute/v1alpha2"
	apispec "github.com/streamnative/function-mesh/pkg/spec"
)

func TestGetValFromPtrOrDefault(t *testing.T) {
	boolVal := true
	boolPtr := &boolVal
	assert.Equal(t, getBoolFromPtrOrDefault(boolPtr, false), boolVal)
	assert.Equal(t, getBoolFromPtrOrDefault(nil, boolVal), boolVal)

	var int32Val int32 = 100
	int32Ptr := &int32Val
	assert.Equal(t, getInt32FromPtrOrDefault(int32Ptr, 200), int32Val)
	assert.Equal(t, getInt32FromPtrOrDefault(nil, int32Val), int32Val)
}

func TestMarshalSecretsMap(t *testing.T) {
	secrets := map[string]apispec.SecretRef{
		"foo": {
			Path: "path",
		},
	}
	marshaledSecrets := marshalSecretsMap(secrets)
	assert.Equal(t, marshaledSecrets, `{"foo":{"path":"path"}}`)

	marshaledSecretsNil := marshalSecretsMap(nil)
	assert.Equal(t, marshaledSecretsNil, `{}`)
}

func TestBatchSource(t *testing.T) {
	source := &computeapi.Source{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Sink",
			APIVersion: "compute.functionmesh.io/v1alpha2",
		},
		ObjectMeta: *makeSampleObjectMeta("test-source"),
		Spec: computeapi.SourceSpec{
			Name:        "test-suorce",
			ClassName:   "org.apache.pulsar.ecosystem.io.bigquery.BigQuerySource",
			Tenant:      "public",
			ClusterName: TestClusterName,
			Output: apispec.OutputConf{
				Topic:         "persistent://public/default/destination",
				TypeClassName: "org.apache.pulsar.common.schema.KeyValue",
				ProducerConf: &apispec.ProducerConfig{
					MaxPendingMessages:                 1000,
					MaxPendingMessagesAcrossPartitions: 50000,
					UseThreadLocalProducers:            true,
				},
			},
			BatchSourceConfig: &computeapi.BatchSourceConfig{
				DiscoveryTriggererClassName: "test-trigger-class",
				DiscoveryTriggererConfig: &apispec.Config{
					Data: map[string]interface{}{
						"test-key": "test-value",
					},
				},
			},
			SourceConfig: &apispec.Config{
				Data: map[string]interface{}{
					"tableName": "test-table",
				},
			},
			Messaging: apispec.Messaging{
				Pulsar: &apispec.PulsarMessaging{
					PulsarConfig: TestClusterName,
					//AuthConfig: "test-auth",
				},
			},
			Image: "test-image",
			Runtime: apispec.Runtime{
				Java: &apispec.JavaRuntime{
					Jar:         "connectors/test.jar",
					JarLocation: "",
				},
			},
		},
	}
	sourceSpec := generateSourceInputSpec(source)
	assert.Equal(t, computeapi.BatchSourceClass, sourceSpec.ClassName)
	assert.Equal(t, `{"__BATCHSOURCECLASSNAME__":"org.apache.pulsar.ecosystem.io.bigquery.BigQuerySource","__BATCHSOURCECONFIGS__":"{\"discoveryTriggererClassName\":\"test-trigger-class\",\"discoveryTriggererConfig\":{\"test-key\":\"test-value\"}}","tableName":"test-table"}`, sourceSpec.Configs)
}
