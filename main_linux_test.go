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

package main_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/utils/checks/checkrequest"

	main "github.com/networkservicemesh/cmd-nsc"
	"github.com/networkservicemesh/cmd-nsc/internal/config"
)

func TestSendFd(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*20))
	defer cancel()

	rootConf := &config.Config{
		Name:      "nsc",
		ConnectTo: url.URL{Scheme: "tcp", Host: "127.0.0.1:0"},
		NetworkServices: []url.URL{
			*must(url.Parse("kernel://my-service/nsmKernel?")),
		},
	}

	testClient := chain.NewNetworkServiceClient(
		sendfd.NewClient(),
		checkrequest.NewClient(t, func(t *testing.T, request *networkservice.NetworkServiceRequest) {
			preferences := request.GetMechanismPreferences()
			require.NotNil(t, preferences)
			require.Equal(t, 1, len(preferences))
			inodeURLString := preferences[0].GetParameters()[common.InodeURL]
			inodeURL, err := url.Parse(inodeURLString)
			require.NoError(t, err)
			require.Equal(t, "inode", inodeURL.Scheme)
		}),
	)

	_, err := main.RunClient(ctx, rootConf, nsmTestClientFactory(testClient))
	require.NoError(t, err)
}
