module github.com/networkservicemesh/cmd-nsc

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/grpcfd v1.1.2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.2.1-0.20220315001249-f33f8c3f2feb
	github.com/networkservicemesh/sdk v0.5.1-0.20220315002012-985d4a0f3ada
	github.com/networkservicemesh/sdk-sriov v0.0.0-20220315003224-4d21ad572176
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-alpha.5
	github.com/stretchr/testify v1.7.0
	google.golang.org/grpc v1.42.0
)

replace github.com/networkservicemesh/sdk => github.com/NikitaSkrynnik/sdk v0.5.1-0.20220311122230-272638498b6c
