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

package main_test

import (
	"context"
	"net/url"
	"testing"

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

var _ networkservice.NetworkServiceClient = (*nsmTestClient)(nil)

func parse(t *testing.T, u string) *config.NetworkServiceConfig {
	c := &config.NetworkServiceConfig{}
	err := c.UnmarshalBinary([]byte(u))
	require.NoError(t, err)
	return c
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
		ConnectTo: &url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []*config.NetworkServiceConfig{
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
		ConnectTo: &url.URL{
			Scheme: "unix",
			Path:   "/file.sock",
		},
		NetworkServices: []*config.NetworkServiceConfig{
			parse(t, "kernel://my-service/nsmKernel?"),
		},
	}
	conns, err := main.RunClient(ctx, cfg, testClient)
	require.NoError(t, err)
	require.NotNil(t, conns)
	require.Equal(t, 1, len(conns))
}
