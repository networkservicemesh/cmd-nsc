// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022 Cisco and/or its affiliates.
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

//go:build linux
// +build linux

package main_test

import (
	"os"
	"testing"

	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"

	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk/pkg/tools/nsurl"

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
