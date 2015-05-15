/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
	"time"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/coreos/go-etcd/etcd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEtcdClient struct {
	// TODO: Convert this to real fs to better simulate etcd behavior.
	writes map[string]string
}

func (ec *fakeEtcdClient) Set(path, value string, ttl uint64) (*etcd.Response, error) {
	ec.writes[path] = value
	return nil, nil
}

func (ec *fakeEtcdClient) Delete(path string, recursive bool) (*etcd.Response, error) {
	for p, _ := range ec.writes {
		if (recursive && strings.HasPrefix(p, path)) || (!recursive && p == path) {
			delete(ec.writes, p)
		}
	}
	return nil, nil
}

const (
	testDomain = "cluster.local"
	basePath   = "/skydns/local/cluster"
)

func newKube2Sky(ec etcdClient) *kube2sky {
	return &kube2sky{
		etcdClient:          ec,
		domain:              testDomain,
		etcdMutationTimeout: time.Second,
	}
}

func TestAddNoServiceIP(t *testing.T) {
	const (
		testService   = "testService"
		testNamespace = "default"
	)
	ec := &fakeEtcdClient{make(map[string]string)}
	k2s := newKube2Sky(ec)
	service := kapi.Service{
		ObjectMeta: kapi.ObjectMeta{
			Name:      testNamespace,
			Namespace: testNamespace,
		},
	}
	k2s.newService(&service)
	assert.Empty(t, ec.writes)
}

func getEtcdPath(name, namespace string) string {
	return path.Join(basePath, namespace, name)
}

type hostPort struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func getHostPort(service *kapi.Service) *hostPort {
	return &hostPort{
		Host: service.Spec.PortalIP,
		Port: service.Spec.Ports[0].Port,
	}
}

func getHostPortFromString(data string) (*hostPort, error) {
	var res hostPort
	err := json.Unmarshal([]byte(data), &res)
	return &res, err
}

func TestAddSinglePortService(t *testing.T) {
	const (
		testService   = "testService"
		testNamespace = "default"
	)
	ec := &fakeEtcdClient{make(map[string]string)}
	k2s := newKube2Sky(ec)
	service := kapi.Service{
		ObjectMeta: kapi.ObjectMeta{
			Name:      testService,
			Namespace: testNamespace,
		},
		Spec: kapi.ServiceSpec{
			Ports: []kapi.ServicePort{
				{
					Port: 80,
				},
			},
			PortalIP: "1.2.3.4",
		},
	}
	k2s.newService(&service)
	expectedKey := getEtcdPath(testService, testNamespace)
	expectedValue := getHostPort(&service)
	val, exists := ec.writes[expectedKey]
	require.True(t, exists)
	actualValue, err := getHostPortFromString(val)
	require.NoError(t, err)
	assert.Equal(t, actualValue, expectedValue)
}

func TestUpdateSinglePortService(t *testing.T) {
	const (
		testService   = "testService"
		testNamespace = "default"
	)
	ec := &fakeEtcdClient{make(map[string]string)}
	k2s := newKube2Sky(ec)
	service := kapi.Service{
		ObjectMeta: kapi.ObjectMeta{
			Name:      testService,
			Namespace: testNamespace,
		},
		Spec: kapi.ServiceSpec{
			Ports: []kapi.ServicePort{
				{
					Port: 80,
				},
			},
			PortalIP: "1.2.3.4",
		},
	}
	k2s.newService(&service)
	assert.Len(t, ec.writes, 1)
	service.Spec.PortalIP = "0.0.0.0"
	k2s.newService(&service)
	expectedKey := getEtcdPath(testService, testNamespace)
	expectedValue := getHostPort(&service)
	val, exists := ec.writes[expectedKey]
	require.True(t, exists)
	actualValue, err := getHostPortFromString(val)
	require.NoError(t, err)
	assert.Equal(t, actualValue, expectedValue)
}

func TestDeleteSinglePortService(t *testing.T) {
	const (
		testService   = "testService"
		testNamespace = "default"
	)
	ec := &fakeEtcdClient{make(map[string]string)}
	k2s := newKube2Sky(ec)
	service := kapi.Service{
		ObjectMeta: kapi.ObjectMeta{
			Name:      testService,
			Namespace: testNamespace,
		},
		Spec: kapi.ServiceSpec{
			Ports: []kapi.ServicePort{
				{
					Port: 80,
				},
			},
			PortalIP: "1.2.3.4",
		},
	}
	// Add the service
	k2s.newService(&service)
	assert.Len(t, ec.writes, 1)
	// Delete the service
	k2s.removeService(&service)
	assert.Empty(t, ec.writes)
}
