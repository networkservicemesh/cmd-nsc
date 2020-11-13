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
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"

	main "github.com/networkservicemesh/cmd-nsc"
	"github.com/networkservicemesh/cmd-nsc/internal/config"
)

func TestSendFd(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*20))
	defer cancel()

	rootConf := &config.Config{
		Name:      "nsc",
		ConnectTo: url.URL{Scheme: "tcp", Host: "127.0.0.1:0"},
		NetworkServices: []config.NetworkServiceConfig{
			*parse(t, "kernel://my-service/nsmKernel?"),
		},
	}

	testClient := chain.NewNetworkServiceClient(
		sendfd.NewClient(),
		&checkConnectionClient{
			T: t,
			check: func(t *testing.T, request *networkservice.NetworkServiceRequest) {
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

	conns, err := main.RunClient(ctx, rootConf, testClient)
	require.NoError(t, err)
	require.NotNil(t, conns)
	require.Equal(t, 1, len(conns))
}

type checkConnectionClient struct {
	*testing.T
	check func(*testing.T, *networkservice.NetworkServiceRequest)
}

func (c *checkConnectionClient) Request(ctx context.Context, request *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error) {
	c.check(c.T, request)
	return next.Client(ctx).Request(ctx, request, opts...)
}

func (c *checkConnectionClient) Close(ctx context.Context, conn *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error) {
	return next.Client(ctx).Close(ctx, conn, opts...)
}

var _ networkservice.NetworkServiceClient = (*checkConnectionClient)(nil)
