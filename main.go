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

// Package main define a nsc application
package main

import (
	"context"
	"fmt"
	"os"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/common"
	kernelmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	vfiomech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/token"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/jaeger"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
	"github.com/networkservicemesh/sdk/pkg/tools/nsurl"
	"github.com/networkservicemesh/sdk/pkg/tools/opentracing"
	"github.com/networkservicemesh/sdk/pkg/tools/signalctx"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"

	"github.com/networkservicemesh/cmd-nsc/internal/config"
)

func main() {
	// ********************************************************************************
	// Configure signal handling context
	// ********************************************************************************
	ctx := signalctx.WithSignals(context.Background())
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()

	// ********************************************************************************
	// Setup logger
	// ********************************************************************************
	logrus.Info("Starting NetworkServiceMesh Client ...")
	logrus.SetFormatter(&nested.Formatter{})
	ctx = log.WithFields(ctx, map[string]interface{}{"cmd": os.Args[:1]})
	ctx = log.WithLog(ctx, logruslogger.New(ctx))

	// ********************************************************************************
	// Configure open tracing
	// ********************************************************************************
	// Enable Jaeger
	log.EnableTracing(true)
	jaegerCloser := jaeger.InitJaeger(ctx, "nsc")
	defer func() { _ = jaegerCloser.Close() }()

	// ********************************************************************************
	// Get config from environment
	// ********************************************************************************
	rootConf := &config.Config{}
	if err := envconfig.Usage("nsm", rootConf); err != nil {
		log.FromContext(ctx).Fatal(err)
	}
	if err := envconfig.Process("nsm", rootConf); err != nil {
		log.FromContext(ctx).Fatalf("error processing rootConf from env: %+v", err)
	}

	log.FromContext(ctx).Infof("rootConf: %+v", rootConf)

	// ********************************************************************************
	// Connect to NSMgr
	// ********************************************************************************
	cleanup, err := RunClient(ctx, rootConf, nsmClientFactory(ctx, rootConf))
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to connect to network services: %v", err.Error())
	} else {
		log.FromContext(ctx).Infof("All client init operations are done.")
	}

	// Wait for cancel event to terminate
	<-ctx.Done()

	// ********************************************************************************
	// Cleanup connections
	// ********************************************************************************
	log.FromContext(ctx).Infof("Performing cleanup of connections due terminate...")

	ctx, cancel = context.WithTimeout(context.Background(), rootConf.DialTimeout)
	defer cancel()

	cleanup(ctx)
}

func nsmClientFactory(ctx context.Context, rootConf *config.Config) func(...networkservice.NetworkServiceClient) networkservice.NetworkServiceClient {
	// ********************************************************************************
	// Get a x509Source
	// ********************************************************************************
	source, err := workloadapi.NewX509Source(ctx)
	if err != nil {
		log.FromContext(ctx).Fatalf("error getting x509 source: %v", err.Error())
	}
	var svid *x509svid.SVID
	svid, err = source.GetX509SVID()
	if err != nil {
		log.FromContext(ctx).Fatalf("error getting x509 svid: %v", err.Error())
	}
	log.FromContext(ctx).Infof("sVID: %q", svid.ID)

	// ********************************************************************************
	// Connect to NSManager
	// ********************************************************************************
	connectCtx, cancel := context.WithTimeout(ctx, rootConf.DialTimeout)
	defer cancel()

	log.FromContext(ctx).Infof("NSC: Connecting to Network Service Manager %v", rootConf.ConnectTo.String())
	var clientCC *grpc.ClientConn
	clientCC, err = grpc.DialContext(
		connectCtx,
		grpcutils.URLToTarget(&rootConf.ConnectTo),
		append(opentracing.WithTracingDial(),
			grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
			grpc.WithTransportCredentials(
				grpcfd.TransportCredentials(
					credentials.NewTLS(
						tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()),
					),
				),
			))...,
	)
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to dial NSM: %v", err.Error())
	}

	// ********************************************************************************
	// Create Network Service Manager nsmClient
	// ********************************************************************************
	tokenClient := token.NewClient()
	sendfdClient := sendfd.NewClient()

	return func(additionalFunctionality ...networkservice.NetworkServiceClient) networkservice.NetworkServiceClient {
		return client.NewClient(
			ctx,
			rootConf.Name,
			nil,
			spiffejwt.TokenGeneratorFunc(source, rootConf.MaxTokenLifetime),
			clientCC,
			append(
				additionalFunctionality,
				tokenClient,
				sendfdClient,
			)...,
		)
	}
}

// RunClient - runs a client application with passed configuration over a client to Network Service Manager
func RunClient(
	ctx context.Context,
	rootConf *config.Config,
	nsmClientFactory func(...networkservice.NetworkServiceClient) networkservice.NetworkServiceClient,
) (cleanup func(context.Context), err error) {
	// Validate config parameters
	if err = rootConf.IsValid(); err != nil {
		return nil, err
	}

	// Setup logging
	ctx = log.WithLog(ctx, logruslogger.New(ctx))

	// ********************************************************************************
	// Initiate connections
	// ********************************************************************************
	for i := range rootConf.NetworkServices {
		connID := fmt.Sprintf("%s-%d", rootConf.Name, i)
		log.FromContext(ctx).Infof("request: %v", connID)

		// Update network services configs
		u := (*nsurl.NSURL)(&rootConf.NetworkServices[i])

		// Construct a request
		request := &networkservice.NetworkServiceRequest{
			Connection: &networkservice.Connection{
				Id:             connID,
				NetworkService: u.NetworkService(),
				Labels:         u.Labels(),
			},
		}

		// Create nsmClient
		var clients []networkservice.NetworkServiceClient

		mech := u.Mechanism()

		switch mech.Type {
		case kernelmech.MECHANISM:
			iface := ""
			if len(u.Mechanism().Parameters) > 0 {
				iface = u.Mechanism().Parameters[common.InterfaceNameKey]
			}
			clients = append(clients, kernel.NewClient(kernel.WithInterfaceName(iface)))
		case vfiomech.MECHANISM:
			clients = append(clients, vfio.NewClient())
		}
		nsmClient := nsmClientFactory(clients...)

		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rootConf.RequestTimeout)
		defer cancel()

		// Performing nsmClient connection request
		conn, err := nsmClient.Request(ctx, request)
		if err != nil {
			log.FromContext(ctx).Errorf("failed to request network service with %v: err %v", request, err.Error())
			continue
		}

		log.FromContext(ctx).Infof("network service established with %v\n Connection: %v", request, conn)

		// Add connection cleanup
		cleanupPrevious := cleanup
		cleanup = func(cleanupCtx context.Context) {
			if cleanupPrevious != nil {
				cleanupPrevious(cleanupCtx)
			}
			_, err := nsmClient.Close(cleanupCtx, conn)
			if err != nil {
				log.FromContext(ctx).Warnf("failed to close connection %v cause: %v", conn, err.Error())
			}
		}
	}

	if cleanup == nil {
		return nil, errors.New("all requests have been failed")
	}
	return cleanup, nil
}
