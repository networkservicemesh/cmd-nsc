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
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/memif"
	main "github.com/networkservicemesh/cmd-nsc"
	"github.com/networkservicemesh/cmd-nsc/pkg/config"
)

type nsmTestClient struct {
	requests []*networkservice.NetworkServiceRequest
	closes   []*networkservice.Connection
}

func (n *nsmTestClient) Request(ctx context.Context, in *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error) {
	n.requests = append(n.requests, in)
	return in.Connection, nil
}

func (n *nsmTestClient) Close(ctx context.Context, in *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error) {
	n.closes = append(n.closes, in)
	return nil, nil
}

type checkConnectionClient struct {
	*testing.T
	check func(*testing.T, context.Context, *networkservice.NetworkServiceRequest)
}

func (c *checkConnectionClient) Request(ctx context.Context, request *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error) {
	c.check(c.T, ctx, request)
	return next.Server(ctx).Request(ctx, request)
}

func (c *checkConnectionClient) Close(ctx context.Context, conn *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error) {
	return next.Server(ctx).Close(ctx, conn)
}

var _ networkservice.NetworkServiceClient = (*nsmTestClient)(nil)
var _ networkservice.NetworkServiceClient = (*checkConnectionClient)(nil)

func parse(t *testing.T, u string) config.NetworkServiceConfig {
	c := config.NetworkServiceConfig{}
	err := c.UnmarshalBinary([]byte(u))
	require.NoError(t, err)
	return c
}

func TestParseUrlsFromEnv(t *testing.T) {
	err := os.Setenv("NSM_NETWORK_SERVICES", "kernel://my-service/nsmKernel,memif://second-service/memif.sock?key=value")
	require.NoError(t, err)

	c := &config.Config{}
	err = envconfig.Process("nsm", c)
	require.NoError(t, err)

	require.Equal(t, 2, len(c.NetworkServices))
	require.Equal(t, kernel.MECHANISM, c.NetworkServices[0].Mechanism)
	require.Equal(t, "nsmKernel", c.NetworkServices[0].Path[0])

	require.Equal(t, memif.MECHANISM, c.NetworkServices[1].Mechanism)
	require.Equal(t, "memif.sock", c.NetworkServices[1].Path[0])
	require.Equal(t, "value", c.NetworkServices[1].Labels["key"])
}

func TestParseNSMUrl(t *testing.T) {
	u1 := parse(t, "kernel://my-service/nsmKernel?a=20")
	require.Equal(t, "my-service", u1.NetworkService)
	require.Equal(t, kernel.MECHANISM, u1.Mechanism)
	require.Equal(t, "nsmKernel", u1.Path[0])
	require.Equal(t, 1, len(u1.Labels))
	require.Equal(t, "20", u1.Labels["a"])
}

func TestMergeOptions(t *testing.T) {
	u1 := parse(t, "my-service/nsmKernel")

	conf := &config.Config{
		Mechanism: "memif",
		Labels:    []string{"A=20"},
	}
	err := u1.MergeWithConfigOptions(conf)
	require.NoError(t, err)
	require.Equal(t, memif.MECHANISM, u1.Mechanism)
	require.Equal(t, 1, len(u1.Labels))
	require.Equal(t, "20", u1.Labels["A"])
}
func TestMergeOptionsNoOverride(t *testing.T) {
	u1 := parse(t, "kernel://my-service/nsmKernel")

	conf := &config.Config{
		Mechanism: "memif",
		Labels:    []string{"A=20"},
	}
	err := u1.MergeWithConfigOptions(conf)
	require.NoError(t, err)
	require.Equal(t, kernel.MECHANISM, u1.Mechanism)
	require.Equal(t, 1, len(u1.Labels))
	require.Equal(t, "20", u1.Labels["A"])
}

func TestConnectNSM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testClient := &nsmTestClient{}
	cfg := &config.Config{
		Name: "nsc",
		ConnectTo: url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []config.NetworkServiceConfig{
			parse(t, "kernel://my-service/nsmKernel?"),
		},
	}
	conns, err := main.RunClient(ctx, cfg, testClient)
	require.NoError(t, err)
	require.NotNil(t, conns)
	require.Equal(t, 1, len(conns))
}

func TestConnectNSMGRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testClient := &nsmTestClient{}
	cfg := &config.Config{
		Name: "nsc",
		ConnectTo: url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []config.NetworkServiceConfig{
			parse(t, "kernel://my-service/nsmKernel?"),
		},
	}
	conns, err := main.RunClient(ctx, cfg, testClient)
	require.NoError(t, err)
	require.NotNil(t, conns)
	require.Equal(t, 1, len(conns))
}

func TestSendFd(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*20))
	defer cancel()

	testClient := chain.NewNetworkServiceClient(
		sendfd.NewClient(),
		&checkConnectionClient{
			T: t,
			check: func(t *testing.T, ctx context.Context, request *networkservice.NetworkServiceRequest) {
				preferences := request.GetMechanismPreferences()
				require.NotNil(t, preferences)
				require.Equal(t, 1, len(preferences))
				inodeURLString := preferences[0].GetParameters()[common.InodeURL]
				inodeURL, err := url.Parse(inodeURLString)
				require.NoError(t, err)
				require.Equal(t, "inode", inodeURL.Scheme)
			},
		},
	)

	cfg := &config.Config{
		Name:      "nsc",
		ConnectTo: url.URL{Scheme: "tcp", Host: "127.0.0.1:0"},
		NetworkServices: []config.NetworkServiceConfig{
			parse(t, "kernel://my-service/nsmKernel?"),
		},
	}

	conns, err := main.RunClient(ctx, cfg, testClient)
	require.NoError(t, err)
	require.NotNil(t, conns)
	require.Equal(t, 1, len(conns))
}
