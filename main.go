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
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	kernelmech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	vfiomech "github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/mechanisms/vfio"
	"github.com/networkservicemesh/sdk-sriov/pkg/networkservice/common/token"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/jaeger"
	"github.com/networkservicemesh/sdk/pkg/tools/logger"
	"github.com/networkservicemesh/sdk/pkg/tools/logger/logruslogger"
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
	ctx, _ = logruslogger.New(
		logger.WithFields(ctx, map[string]interface{}{"cmd": os.Args[:1]}),
	)

	// ********************************************************************************
	// Configure open tracing
	// ********************************************************************************
	// Enable Jaeger
	logger.EnableTracing(true)
	jaegerCloser := jaeger.InitJaeger("nsc")
	defer func() { _ = jaegerCloser.Close() }()

	// ********************************************************************************
	// Get config from environment
	// ********************************************************************************
	rootConf := &config.Config{}
	if err := envconfig.Usage("nsm", rootConf); err != nil {
		logger.Log(ctx).Fatal(err)
	}
	if err := envconfig.Process("nsm", rootConf); err != nil {
		logger.Log(ctx).Fatalf("error processing rootConf from env: %+v", err)
	}

	logger.Log(ctx).Infof("rootConf: %+v", rootConf)

	// ********************************************************************************
	// Connect to NSMgr
	// ********************************************************************************
	cleanup, err := RunClient(ctx, rootConf, nsmClientFactory(ctx, rootConf))
	if err != nil {
		logger.Log(ctx).Fatalf("failed to connect to network services: %v", err.Error())
	} else {
		logger.Log(ctx).Infof("All client init operations are done.")
	}

	// Wait for cancel event to terminate
	<-ctx.Done()

	// ********************************************************************************
	// Cleanup connections
	// ********************************************************************************
	logger.Log(ctx).Infof("Performing cleanup of connections due terminate...")

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
		logger.Log(ctx).Fatalf("error getting x509 source: %v", err.Error())
	}
	var svid *x509svid.SVID
	svid, err = source.GetX509SVID()
	if err != nil {
		logger.Log(ctx).Fatalf("error getting x509 svid: %v", err.Error())
	}
	logger.Log(ctx).Infof("sVID: %q", svid.ID)

	// ********************************************************************************
	// Connect to NSManager
	// ********************************************************************************
	connectCtx, cancel := context.WithTimeout(ctx, rootConf.DialTimeout)
	defer cancel()

	logger.Log(ctx).Infof("NSC: Connecting to Network Service Manager %v", rootConf.ConnectTo.String())
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
		logger.Log(ctx).Fatalf("failed to dial NSM: %v", err.Error())
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
	ctx, _ = logruslogger.New(ctx)

	// ********************************************************************************
	// Initiate connections
	// ********************************************************************************
	for i := range rootConf.NetworkServices {
		connID := fmt.Sprintf("%s-%d", rootConf.Name, i)
		logger.Log(ctx).Infof("request: %v", connID)

		// Update network services configs
		nsConf := &rootConf.NetworkServices[i]
		if err = nsConf.MergeWithConfigOptions(rootConf); err != nil {
			logger.Log(ctx).Errorf("error during nsmClient config aggregation: %v", err.Error())
			continue
		}

		// Construct a request
		request := &networkservice.NetworkServiceRequest{
			Connection: &networkservice.Connection{
				Id:             connID,
				NetworkService: nsConf.NetworkService,
				Labels:         nsConf.Labels,
			},
		}

		// Create nsmClient
		var clients []networkservice.NetworkServiceClient
		switch nsConf.Mechanism {
		case kernelmech.MECHANISM:
			clients = append(clients, kernel.NewClient(kernel.WithInterfaceName(nsConf.Path[0])))
		case vfiomech.MECHANISM:
			var cgroupDir string
			cgroupDir, err = cgroupDirPath()
			if err != nil {
				logger.Log(ctx).Errorf("failed to get devices cgroup: %v", err.Error())
				continue
			}
			clients = append(clients, vfio.NewClient("/dev/vfio", cgroupDir))
		}
		nsmClient := nsmClientFactory(clients...)

		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rootConf.RequestTimeout)
		defer cancel()

		// Performing nsmClient connection request
		conn, err := nsmClient.Request(ctx, request)
		if err != nil {
			logger.Log(ctx).Errorf("failed to request network service with %v: err %v", request, err.Error())
			continue
		}

		logger.Log(ctx).Infof("network service established with %v\n Connection: %v", request, conn)

		// Add connection cleanup
		cleanupPrevious := cleanup
		cleanup = func(cleanupCtx context.Context) {
			if cleanupPrevious != nil {
				cleanupPrevious(cleanupCtx)
			}
			_, err := nsmClient.Close(cleanupCtx, conn)
			if err != nil {
				logger.Log(ctx).Warnf("failed to close connection %v cause: %v", conn, err.Error())
			}
		}
	}

	if cleanup == nil {
		return nil, errors.New("all requests have been failed")
	}
	return cleanup, nil
}

var devicesCgroup = regexp.MustCompile("^[1-9][0-9]*?:devices:(.*)$")

func cgroupDirPath() (string, error) {
	cgroupInfo, err := os.OpenFile("/proc/self/cgroup", os.O_RDONLY, 0)
	if err != nil {
		return "", errors.Wrap(err, "error opening cgroup info file")
	}
	for scanner := bufio.NewScanner(cgroupInfo); scanner.Scan(); {
		line := scanner.Text()
		if devicesCgroup.MatchString(line) {
			return podCgroupDirPath(devicesCgroup.FindStringSubmatch(line)[1]), nil
		}
	}
	return "", errors.New("can't find out cgroup directory")
}

func podCgroupDirPath(containerCgroupDirPath string) string {
	split := strings.Split(containerCgroupDirPath, string(filepath.Separator))
	split[len(split)-1] = "*" // any container match
	return filepath.Join(split...)
}
