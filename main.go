// Copyright (c) 2020-2022 Doc.ai and/or its affiliates.
// Copyright (c) 2021-2022 Nordix and/or its affiliates.
//
// Copyright (c) 2022-2024 Cisco and/or its affiliates.
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

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/genericsync"
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
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clientinfo"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/excludedprefixes"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/heal"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/null"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/upstreamrefresh"
	"github.com/networkservicemesh/sdk/pkg/networkservice/connectioncontext/dnscontext"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/cache"
	dnschain "github.com/networkservicemesh/sdk/pkg/tools/dnsutils/chain"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/checkmsg"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/dnsconfigs"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/fanout"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/noloop"
	"github.com/networkservicemesh/sdk/pkg/tools/dnsutils/searches"
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
		metricExporter := opentelemetry.InitOPTLMetricExporter(ctx, collectorAddress, c.MetricsExportInterval)
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

	tlsClientConfig := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny())
	tlsClientConfig.MinVersion = tls.VersionTLS12

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
				credentials.NewTLS(tlsClientConfig),
			),
		),
	)

	dnsClient := null.NewClient()
	if c.LocalDNSServerEnabled {
		dnsConfigsMap := new(genericsync.Map[string, []*networkservice.DNSConfig])
		dnsClient = dnscontext.NewClient(dnscontext.WithChainContext(ctx), dnscontext.WithDNSConfigsMap(dnsConfigsMap))

		dnsServerHandler := dnschain.NewDNSHandler(
			checkmsg.NewDNSHandler(),
			dnsconfigs.NewDNSHandler(dnsConfigsMap),
			searches.NewDNSHandler(),
			noloop.NewDNSHandler(),
			cache.NewDNSHandler(),
			fanout.NewDNSHandler(),
		)

		go dnsutils.ListenAndServe(ctx, dnsServerHandler, c.LocalDNSServerAddress)
	}

	var healOptions = []heal.Option{heal.WithLivenessCheckInterval(c.LivenessCheckInterval),
		heal.WithLivenessCheckTimeout(c.LivenessCheckTimeout)}

	if c.LivenessCheckEnabled {
		healOptions = append(healOptions, heal.WithLivenessCheck(kernelheal.KernelLivenessCheck))
	}

	nsmClient := client.NewClient(ctx,
		client.WithClientURL(&c.ConnectTo),
		client.WithName(c.Name),
		client.WithAuthorizeClient(authorize.NewClient()),
		client.WithHealClient(heal.NewClient(ctx, healOptions...)),
		client.WithAdditionalFunctionality(
			clientinfo.NewClient(),
			upstreamrefresh.NewClient(ctx),
			sriovtoken.NewClient(),
			mechanisms.NewClient(map[string]networkservice.NetworkServiceClient{
				vfiomech.MECHANISM:   chain.NewNetworkServiceClient(vfio.NewClient()),
				kernelmech.MECHANISM: chain.NewNetworkServiceClient(kernel.NewClient()),
			}),
			sendfd.NewClient(),
			dnsClient,
			excludedprefixes.NewClient(excludedprefixes.WithAwarenessGroups(c.AwarenessGroups)),
		),
		client.WithDialTimeout(c.DialTimeout),
		client.WithDialOptions(dialOptions...),
	)

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
		id := fmt.Sprintf("%s-%d", c.Name, i)

		var monitoredConnections genericsync.Map[string, *networkservice.Connection]
		monitorCtx, cancelMonitor := context.WithTimeout(signalCtx, c.RequestTimeout)
		stream, err := monitorClient.MonitorConnections(monitorCtx, &networkservice.MonitorScopeSelector{
			PathSegments: []*networkservice.PathSegment{
				{
					Id: id,
				},
			},
		})
		if err != nil {
			logger.Fatalf("error from monitorConnectionClient: %v", err.Error())
		}

		// Recv initial event
		event, err := stream.Recv()
		if err != nil {
			logger.Errorf("error from monitorConnection stream: %v ", err.Error())
		}
		for k, conn := range event.Connections {
			monitoredConnections.Store(k, conn)
		}

		go func() {
			for {
				event, err := stream.Recv()
				if err != nil {
					break
				}
				for k, conn := range event.Connections {
					monitoredConnections.Store(k, conn)
				}
			}
		}()

		for {
			// Construct a request
			request := constructRequest(ctx, c, id, &c.NetworkServices[i], &monitoredConnections)

			resp, err := nsmClient.Request(ctx, request)
			if err != nil {
				logger.Errorf("failed connect to NSMgr: %v", err.Error())
				continue
			}

			defer func() {
				closeCtx, cancelClose := context.WithTimeout(ctx, c.RequestTimeout)
				defer cancelClose()
				_, _ = nsmClient.Close(closeCtx, resp)
			}()

			logger.Infof("successfully connected to %v. Response: %v", resp.NetworkService, resp)
			break
		}
		cancelMonitor()
	}

	// Wait for cancel event to terminate
	<-signalCtx.Done()
}

func constructRequest(ctx context.Context, c *config.Config, connectionID string, networkService *url.URL, monitoredConnections *genericsync.Map[string, *networkservice.Connection]) *networkservice.NetworkServiceRequest {
	u := (*nsurl.NSURL)(networkService)

	request := &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			Id:             connectionID,
			NetworkService: u.NetworkService(),
			Labels:         u.Labels(),
		},
		MechanismPreferences: []*networkservice.Mechanism{
			u.Mechanism(),
		},
	}

	// Looking for a match in the connections received from monitoring
	monitoredConnections.Range(func(key string, conn *networkservice.Connection) bool {
		path := conn.GetPath()
		if path.Index == 1 && path.PathSegments[0].Id == connectionID && conn.Mechanism.Type == u.Mechanism().Type {
			request.Connection = conn
			request.Connection.Path.Index = 0
			request.Connection.Id = connectionID
			return false
		}
		return true
	})

	lCheckCtx, lCheckCtxCancel := context.WithTimeout(ctx, c.LivenessCheckTimeout)
	defer lCheckCtxCancel()
	if request.GetConnection().State == networkservice.State_DOWN &&
		(!c.LivenessCheckEnabled || !kernelheal.KernelLivenessCheck(lCheckCtx, request.GetConnection())) {
		// We cannot Close this because the connection was not established through this chain.
		// We can only reselect an endpoint
		log.FromContext(ctx).Infof("NetworkServiceEndpoint %v is unavailable. Reconnection...", request.GetConnection().NetworkServiceEndpointName)
		request.GetConnection().Mechanism = nil
		request.GetConnection().NetworkServiceEndpointName = ""
		request.GetConnection().Context = nil
		request.GetConnection().State = networkservice.State_RESELECT_REQUESTED
	}
	return request
}
