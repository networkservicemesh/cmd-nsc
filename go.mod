module github.com/networkservicemesh/cmd-nsc

go 1.15

require (
	github.com/antonfisher/nested-logrus-formatter v1.1.0
	github.com/edwarnicke/grpcfd v0.0.0-20210219150442-10fb469a6976
	github.com/golang/protobuf v1.4.3
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v0.0.0-20210218170701-1a72f1cba074
	github.com/networkservicemesh/sdk v0.0.0-20210220122417-bab01203bb73
	github.com/networkservicemesh/sdk-sriov v0.0.0-20210220123356-bb4d2774374a
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spiffe/go-spiffe/v2 v2.0.0-alpha.5
	github.com/stretchr/testify v1.6.1
	google.golang.org/grpc v1.35.0
)

replace github.com/networkservicemesh/sdk => ../sdk
