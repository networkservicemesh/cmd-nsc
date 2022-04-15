module github.com/networkservicemesh/cmd-nsc

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/grpcfd v1.1.2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.3.0-rc.1.0.20220405210054-fbcde048efa5
	github.com/networkservicemesh/sdk v0.5.1-0.20220505102418-8d6762737896
	github.com/networkservicemesh/sdk-sriov v0.0.0-20220505140026-a0c110da4f69
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0
	github.com/stretchr/testify v1.7.0
	google.golang.org/grpc v1.42.0
)

replace github.com/networkservicemesh/sdk => github.com/xzfc/networkservicemesh-sdk v0.0.0-20220414232223-3a19549f4efa

replace github.com/networkservicemesh/sdk-kernel => github.com/xzfc/networkservicemesh-sdk-kernel v0.0.0-20220415114510-21e40d2cba48
