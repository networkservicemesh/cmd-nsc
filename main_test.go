// Copyright (c) 2020 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build !windows

package main_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/tools/yamlhelper"

	main "github.com/networkservicemesh/cmd-nsc"
	"github.com/networkservicemesh/cmd-nsc/pkg/config"
)

const (
	requestsFileName = "test/requests.yml"
)

func TestParseUrlsFromEnv(t *testing.T) {
	err := os.Setenv("NSM_NETWORK_SERVICES", "kernel://my-service/nsmKernel,vfio://second-service?sriovToken=intel/10G")
	require.NoError(t, err)

	c := &config.Config{}
	err = envconfig.Process("nsm", c)
	require.NoError(t, err)

	require.Equal(t, 2, len(c.NetworkServices))
	require.Equal(t, kernel.MECHANISM, c.NetworkServices[0].Mechanism)
	require.Equal(t, "nsmKernel", c.NetworkServices[0].Path[0])

	require.Equal(t, vfio.MECHANISM, c.NetworkServices[1].Mechanism)
	require.Equal(t, "intel/10G", c.NetworkServices[1].Labels["sriovToken"])
}

func TestParseNSMUrl(t *testing.T) {
	nsmConf := parse(t, "kernel://my-service/nsmKernel?A=20")

	require.Equal(t, &config.NetworkServiceConfig{
		NetworkService: "my-service",
		Path:           []string{"nsmKernel"},
		Mechanism:      kernel.MECHANISM,
		Labels: map[string]string{
			"A": "20",
		},
	}, nsmConf)
}

func TestMergeOptions(t *testing.T) {
	rootConf := &config.Config{
		Mechanism: "vfio",
		Labels:    []string{"A=20"},
	}

	nsmConf := parse(t, "my-service?B=40")
	require.NoError(t, nsmConf.MergeWithConfigOptions(rootConf))

	require.Equal(t, &config.NetworkServiceConfig{
		NetworkService: "my-service",
		Path:           []string{},
		Mechanism:      vfio.MECHANISM,
		Labels: map[string]string{
			"A": "20",
			"B": "40",
		},
	}, nsmConf)
}

func TestMergeOptionsNoOverride(t *testing.T) {
	rootConf := &config.Config{
		Mechanism: "vfio",
		Labels:    []string{"A=20"},
	}

	nsmConf := parse(t, "kernel://my-service/nsmKernel?B=40")
	require.NoError(t, nsmConf.MergeWithConfigOptions(rootConf))

	require.Equal(t, &config.NetworkServiceConfig{
		NetworkService: "my-service",
		Path:           []string{"nsmKernel"},
		Mechanism:      kernel.MECHANISM,
		Labels: map[string]string{
			"A": "20",
			"B": "40",
		},
	}, nsmConf)
}

func TestConnectNSM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests []*networkservice.NetworkServiceRequest
	err := yamlhelper.UnmarshalFile(requestsFileName, &requests)
	require.NoError(t, err)

	rootConf := &config.Config{
		Name: "nsc",
		ConnectTo: url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []config.NetworkServiceConfig{
			*parse(t, "kernel://my-service/if-1?label-1=value-1"),
			*parse(t, "kernel://my-service/if-2?label-1=value-1"),
			*parse(t, "kernel://my-service/if-3?label-2=value-2"),
			*parse(t, "kernel://service/if-4?label-2=value-2"),
		},
	}

	testClient := &nsmTestClient{}

	_, err = main.RunClient(ctx, rootConf, testClient)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(requests), fmt.Sprint(testClient.requests))
}

func parse(t *testing.T, u string) *config.NetworkServiceConfig {
	c := &config.NetworkServiceConfig{}
	err := c.UnmarshalBinary([]byte(u))
	require.NoError(t, err)
	return c
}

type nsmTestClient struct {
	requests []*networkservice.NetworkServiceRequest
	closes   []*networkservice.Connection
}

func (n *nsmTestClient) Request(_ context.Context, request *networkservice.NetworkServiceRequest, _ ...grpc.CallOption) (*networkservice.Connection, error) {
	n.requests = append(n.requests, request)
	return request.Connection, nil
}

func (n *nsmTestClient) Close(_ context.Context, conn *networkservice.Connection, _ ...grpc.CallOption) (*empty.Empty, error) {
	n.closes = append(n.closes, conn)
	return &empty.Empty{}, nil
}

var _ networkservice.NetworkServiceClient = (*nsmTestClient)(nil)
