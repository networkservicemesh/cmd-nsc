// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
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
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/cls"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/nsurl"

	main "github.com/networkservicemesh/cmd-nsc"
	"github.com/networkservicemesh/cmd-nsc/internal/config"
)

func TestParseUrlsFromEnv(t *testing.T) {
	err := os.Setenv("NSM_NETWORK_SERVICES", "kernel://my-service/nsmKernel,vfio://second-service?sriovToken=intel/10G")
	require.NoError(t, err)

	c := &config.Config{}
	err = envconfig.Process("nsm", c)
	require.NoError(t, err)

	require.Equal(t, 2, len(c.NetworkServices))

	url1 := nsurl.NSURL(c.NetworkServices[0])
	url2 := nsurl.NSURL(c.NetworkServices[1])

	require.Equal(t, kernel.MECHANISM, url1.Mechanism().Type)
	require.Equal(t, "nsmKernel", url1.Mechanism().GetParameters()[common.InterfaceNameKey])

	require.Equal(t, vfio.MECHANISM, url2.Mechanism().Type)
	require.Equal(t, "intel/10G", url2.Labels()["sriovToken"])
}

func must(u *url.URL, err error) *url.URL {
	if err != nil {
		panic(err.Error())
	}
	return u
}

func TestRunClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests = []*networkservice.NetworkServiceRequest{
		{
			Connection: &networkservice.Connection{
				Id:             "nsc-0",
				NetworkService: "my-service",
				Labels: map[string]string{
					"label-1": "value-1",
				},
			},
			MechanismPreferences: []*networkservice.Mechanism{
				{
					Cls:  cls.LOCAL,
					Type: kernel.MECHANISM,
					Parameters: map[string]string{
						kernel.NetNSURL:         "file:///proc/thread-self/ns/net",
						kernel.InterfaceNameKey: "if-1",
					},
				},
			},
		},
		{
			Connection: &networkservice.Connection{
				Id:             "nsc-1",
				NetworkService: "service",
				Labels: map[string]string{
					"label-2": "value-2",
				},
			},
			MechanismPreferences: []*networkservice.Mechanism{
				{
					Cls:  cls.LOCAL,
					Type: kernel.MECHANISM,
					Parameters: map[string]string{
						kernel.NetNSURL:         "file:///proc/thread-self/ns/net",
						kernel.InterfaceNameKey: "if-2",
					},
				},
			},
		},
	}

	rootConf := &config.Config{
		Name: "nsc",
		ConnectTo: url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []url.URL{
			*must(url.Parse("kernel://my-service/if-1?label-1=value-1")),
			*must(url.Parse("kernel://service/if-2?label-2=value-2")),
		},
	}

	testClient := new(nsmTestClient)

	cleanup, err := main.RunClient(ctx, rootConf, nsmTestClientFactory(testClient))
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(requests), fmt.Sprint(testClient.requests))

	var closes []*networkservice.Connection
	for _, request := range requests {
		closes = append(closes, request.Connection.Clone())
	}

	cleanup(ctx)
	require.Equal(t, fmt.Sprint(closes), fmt.Sprint(testClient.closes))
}

func nsmTestClientFactory(testClient networkservice.NetworkServiceClient) func(...networkservice.NetworkServiceClient) networkservice.NetworkServiceClient {
	return func(additionalFunctionality ...networkservice.NetworkServiceClient) networkservice.NetworkServiceClient {
		return next.NewNetworkServiceClient(chain.NewNetworkServiceClient(additionalFunctionality...), chain.NewNetworkServiceClient(testClient))
	}
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
