// Copyright (c) 2021 Doc.ai and/or its affiliates.
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
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/exechelper"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/credentials"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/null"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
	"github.com/networkservicemesh/sdk/pkg/tools/sandbox"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/networkservicemesh/sdk/pkg/tools/spire"
	"github.com/networkservicemesh/sdk/pkg/tools/token"
)

func Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	var spireErrCh, sutErrCh <-chan error
	t.Cleanup(func() {
		cancel()
		if spireErrCh != nil {
			for range spireErrCh {
			}
		}
		if sutErrCh != nil {
			for range sutErrCh {
			}
		}
	})

	logrus.SetFormatter(&nested.Formatter{})
	log.EnableTracing(true)
	ctx = log.Join(ctx, logruslogger.New(ctx))

	// --------------------------------------------------------------------------
	log.FromContext(ctx).Info("Start spire")
	// --------------------------------------------------------------------------
	executable, err := os.Executable()
	require.NoError(t, err)

	spireErrCh = spire.Start(
		spire.WithContext(ctx),
		spire.WithEntry("spiffe://example.org/nsc", "unix:path:/bin/nsc"),
		spire.WithEntry(fmt.Sprintf("spiffe://example.org/%s", filepath.Base(executable)),
			fmt.Sprintf("unix:path:%s", executable),
		),
	)
	require.Len(t, spireErrCh, 0)

	// --------------------------------------------------------------------------
	log.FromContext(ctx).Info("Get X509Source")
	// --------------------------------------------------------------------------
	source, err := workloadapi.NewX509Source(ctx)
	require.NoError(t, err)

	// --------------------------------------------------------------------------
	log.FromContext(ctx).Info("Start NSM")
	// --------------------------------------------------------------------------
	domain := sandbox.NewBuilder(ctx, t).
		SetNodeSetup(func(ctx context.Context, node *sandbox.Node, i int) {
			// --------------------------------------------------------------------------
			log.FromContext(ctx).Info("Start NSMgr")
			// --------------------------------------------------------------------------
			node.NewNSMgr(ctx, "nsmgr")

			// --------------------------------------------------------------------------
			log.FromContext(ctx).Info("Start Forwarder")
			// --------------------------------------------------------------------------
			forwarderReg := &registry.NetworkServiceEndpoint{
				Name: "forwarder",
			}
			node.NewForwarder(ctx, forwarderReg,
				sandbox.WithForwarderAdditionalServerFunctionality(
					mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
						kernel.MECHANISM: null.NewServer(),
					}),
				),
			)
		}).
		SetServerTransportCredentialsSupplier(func() credentials.TransportCredentials {
			return credentials.NewTLS(tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeAny()))
		}).
		SetClientTransportCredentialsSupplier(func() credentials.TransportCredentials {
			return credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()))
		}).
		SetTokenGeneratorSupplier(func(timeout time.Duration) token.GeneratorFunc {
			return spiffejwt.TokenGeneratorFunc(source, timeout)
		}).
		UseUnixSockets().
		Build()

	// --------------------------------------------------------------------------
	log.FromContext(ctx).Info("Start Endpoint")
	// --------------------------------------------------------------------------
	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "nse",
		NetworkServiceNames: []string{"ns"},
	}

	counter := new(counterServer)
	domain.Nodes[0].NewEndpoint(ctx, nseReg,
		sandbox.WithEndpointAdditionalFunctionality(counter),
	)

	// --------------------------------------------------------------------------
	log.FromContext(ctx).Info("Start Client")
	// --------------------------------------------------------------------------
	nscCtx, nscCancel := context.WithCancel(ctx)

	cmdStr := "nsc"
	sutErrCh = exechelper.Start(cmdStr,
		exechelper.WithContext(nscCtx),
		exechelper.WithEnvirons(os.Environ()...),
		exechelper.WithStdout(os.Stdout),
		exechelper.WithStderr(os.Stderr),
		exechelper.WithGracePeriod(time.Minute),
		exechelper.WithEnvKV("NSM_NAME", "nsc"),
		exechelper.WithEnvKV("NSM_CONNECT_TO", domain.Nodes[0].NSMgr.URL.String()),
		exechelper.WithEnvKV("NSM_NETWORK_SERVICES", "kernel://ns"),
	)
	require.Len(t, sutErrCh, 0)

	time.Sleep(15 * time.Second)
	require.Equal(t, int32(1), counter.Requests)

	nscCancel()
	for range sutErrCh {
	}

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&counter.Closes) == 1
	}, 15*time.Second, time.Second)
}

type counterServer struct {
	Requests, Closes int32
}

func (s *counterServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	atomic.AddInt32(&s.Requests, 1)

	return next.Server(ctx).Request(ctx, request)
}

func (s *counterServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	atomic.AddInt32(&s.Closes, 1)

	return next.Server(ctx).Close(ctx, conn)
}
