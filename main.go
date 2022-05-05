// Copyright (c) 2020-2022 Doc.ai and/or its affiliates.
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

// Package main define a nsc application
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	kernelheal "github.com/networkservicemesh/sdk-kernel/pkg/kernel/tools/heal"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	kernelmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	vfiomech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/mechanisms/vfio"
	sriovtoken "github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/token"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/excludedprefixes"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/heal"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/retry"
	"github.com/networkservicemesh/sdk/pkg/networkservice/connectioncontext/dnscontext"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
	"github.com/networkservicemesh/sdk/pkg/tools/nsurl"
	"github.com/networkservicemesh/sdk/pkg/tools/opentelemetry"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/networkservicemesh/sdk/pkg/tools/token"
	"github.com/networkservicemesh/sdk/pkg/tools/tracing"

	"github.com/networkservicemesh/cmd-nsc/internal/config"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ********************************************************************************
	// Setup logger
	// ********************************************************************************
	log.EnableTracing(true)
	logrus.Info("Starting NetworkServiceMesh Client ...")
	logrus.SetFormatter(&nested.Formatter{})
	ctx = log.WithLog(ctx, logruslogger.New(ctx, map[string]interface{}{"cmd": os.Args[:1]}))

	logger := log.FromContext(ctx)

	// ********************************************************************************
	// Get config from environment
	// ********************************************************************************
	c := &config.Config{}
	if err := envconfig.Usage("nsm", c); err != nil {
		logger.Fatal(err)
	}
	if err := envconfig.Process("nsm", c); err != nil {
		logger.Fatalf("error processing rootConf from env: %+v", err)
	}

	level, err := logrus.ParseLevel(c.LogLevel)
	if err != nil {
		logrus.Fatalf("invalid log level %s", c.LogLevel)
	}
	logrus.SetLevel(level)

	logger.Infof("rootConf: %+v", c)

	// ********************************************************************************
	// Configure Open Telemetry
	// ********************************************************************************
	if opentelemetry.IsEnabled() {
		collectorAddress := c.OpenTelemetryEndpoint
		spanExporter := opentelemetry.InitSpanExporter(ctx, collectorAddress)
		metricExporter := opentelemetry.InitMetricExporter(ctx, collectorAddress)
		o := opentelemetry.Init(ctx, spanExporter, metricExporter, c.Name)
		defer func() {
			if err = o.Close(); err != nil {
				logger.Error(err.Error())
			}
		}()
	}

	// ********************************************************************************
	// Get a x509Source
	// ********************************************************************************
	source, err := workloadapi.NewX509Source(ctx)
	if err != nil {
		logger.Fatalf("error getting x509 source: %v", err.Error())
	}
	var svid *x509svid.SVID
	svid, err = source.GetX509SVID()
	if err != nil {
		logger.Fatalf("error getting x509 svid: %v", err.Error())
	}
	logger.Infof("sVID: %q", svid.ID)

	// ********************************************************************************
	// Create Network Service Manager nsmClient
	// ********************************************************************************
	dialOptions := append(tracing.WithTracingDial(),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor(),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, c.MaxTokenLifetime))),
		),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(
				credentials.NewTLS(
					tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()),
				),
			),
		),
	)

	nsmClient := client.NewClient(ctx,
		client.WithClientURL(&c.ConnectTo),
		client.WithName(c.Name),
		client.WithAuthorizeClient(authorize.NewClient()),
		client.WithHealClient(
			heal.NewClient(
				ctx,
				heal.WithLivenessCheck(kernelheal.NewKernelLivenessCheck),
				heal.WithLivenessCheckInterval(c.LivenessCheckInterval),
				heal.WithLivenessCheckTimeout(c.LivenessCheckTimeout))),
		client.WithAdditionalFunctionality(
			sriovtoken.NewClient(),
			mechanisms.NewClient(map[string]networkservice.NetworkServiceClient{
				vfiomech.MECHANISM:   chain.NewNetworkServiceClient(vfio.NewClient()),
				kernelmech.MECHANISM: chain.NewNetworkServiceClient(kernel.NewClient()),
			}),
			sendfd.NewClient(),
			dnscontext.NewClient(dnscontext.WithChainContext(ctx)),
			excludedprefixes.NewClient(excludedprefixes.WithAwarenessGroups(c.AwarenessGroups)),
		),
		client.WithDialTimeout(c.DialTimeout),
		client.WithDialOptions(dialOptions...),
	)

	nsmClient = retry.NewClient(nsmClient, retry.WithTryTimeout(c.RequestTimeout))

	// ********************************************************************************
	// Configure signal handling context
	// ********************************************************************************
	signalCtx, cancelSignalCtx := signal.NotifyContext(
		ctx,
		os.Interrupt,
		// More Linux signals here
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer cancelSignalCtx()

	// ********************************************************************************
	// Create Network Service Manager monitorClient
	// ********************************************************************************
	dialCtx, cancelDial := context.WithTimeout(signalCtx, c.DialTimeout)
	defer cancelDial()

	logger.Infof("NSC: Connecting to Network Service Manager %v", c.ConnectTo.String())
	cc, err := grpc.DialContext(dialCtx, grpcutils.URLToTarget(&c.ConnectTo), dialOptions...)
	if err != nil {
		logger.Fatalf("failed dial to NSMgr: %v", err.Error())
	}

	monitorClient := networkservice.NewMonitorConnectionClient(cc)

	// ********************************************************************************
	// Initiate connections
	// ********************************************************************************
	for i := 0; i < len(c.NetworkServices); i++ {
		// Update network services configs
		u := (*nsurl.NSURL)(&c.NetworkServices[i])

		id := fmt.Sprintf("%s-%d", c.Name, i)

		monitorCtx, cancelMonitor := context.WithTimeout(signalCtx, c.RequestTimeout)
		defer cancelMonitor()

		stream, err := monitorClient.MonitorConnections(monitorCtx, &networkservice.MonitorScopeSelector{
			PathSegments: []*networkservice.PathSegment{
				{
					Id: id,
				},
			},
		})
		if err != nil {
			logger.Fatal(err.Error())
		}

		event, err := stream.Recv()
		if err != nil {
			logger.Fatal(err.Error())
		}
		cancelMonitor()

		// Construct a request
		request := &networkservice.NetworkServiceRequest{
			Connection: &networkservice.Connection{
				Id:             id,
				NetworkService: u.NetworkService(),
				Labels:         u.Labels(),
			},
			MechanismPreferences: []*networkservice.Mechanism{
				u.Mechanism(),
			},
		}

		for _, conn := range event.Connections {
			path := conn.GetPath()
			if path.Index == 1 && path.PathSegments[0].Id == id && conn.Mechanism.Type == u.Mechanism().Type {
				request.Connection = conn
				request.Connection.Path.Index = 0
				request.Connection.Id = id
				break
			}
		}

		resp, err := nsmClient.Request(ctx, request)
		if err != nil {
			logger.Fatalf("failed connect to NSMgr: %v", err.Error())
		}

		defer func() {
			closeCtx, cancelClose := context.WithTimeout(ctx, c.RequestTimeout)
			defer cancelClose()
			_, _ = nsmClient.Close(closeCtx, resp)
		}()

		logger.Infof("successfully connected to %v. Response: %v", u.NetworkService(), resp)
	}

	// Wait for cancel event to terminate
	<-signalCtx.Done()
}
