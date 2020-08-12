# Intro

This repo contains 'cmd-nsc' a client application for Network Service Mesh.

This README will provide directions for building, testing, and debugging that container.

# Usage

`cmd-nsc` accept following environment variables:

* NSM_NAME - A string value of network service client name (default "nsc")
* NSM_CONNECT_TO - A Network service Manager connectTo URL (default "unix:///var/lib/networkservicemesh/nsm.io.sock")
* NSM_MAX_TOKEN_LIFETIME -  A token lifetime duration (default 24h)                                                            
* NSM_ROUTES - A comma separated list of routes.                                                                                 
* NSM_LABELS - A list of client labels with format key1=val1,key2=val2, will be used a primary list for network services
* NSM_MECHANISM - Default Mechanism to use, supported values "kernel","memif"                                                         
* NSM_NETWORK_SERVICES - A list of Network Service Requests URLs with inner format 
    - [{mechanism}]?:${nsName}[@domainName]?/${interfaceName/memIfSocketName}?${label1}=${value1}&${label2}=${value2}
        - mechanism one of "kernel", "memif"
        - nsName - a Network service name requested
        - domainName - a interdomain service name
        - interfaceName - a kernel interface name prefix, for kernel mechanism.
        - memIfSocketName - a memif socket name to use for memif mechanism.
        - labelX/valueX - a pairs of labels will be passed as part of request.

# Build

## Build nsmgr binary locally

You can build the locally by executing

```bash
go build ./...
```

## Build Docker container

You can build the docker container by running:

```bash
docker build .
```

# Testing

## Testing Docker container

Testing is run via a Docker container.  To run testing run:

```bash
docker run --rm $(docker build -q --target test .)
```

# Debugging

## Debugging the tests
If you wish to debug the test code itself, that can be acheived by running:

```bash
docker run --rm -p 40000:40000 $(docker build -q --target debug .)
```

This will result in the tests running under dlv.  Connecting your debugger to localhost:40000 will allow you to debug.

```bash
-p 40000:40000
```
forwards port 40000 in the container to localhost:40000 where you can attach with your debugger.

```bash
--target debug
```

Runs the debug target, which is just like the test target, but starts tests with dlv listening on port 40000 inside the container.

## Debugging

When you run 'nsc' you will see an early line of output that tells you:

```Setting env variable DLV_LISTEN_FORWARDER to a valid dlv '--listen' value will cause the dlv debugger to execute this binary and listen as directed.```

If you follow those instructions when running the Docker container:
```bash
docker run -e DLV_LISTEN_NSMGR=:50000 -p 50000:50000 --rm $(docker build -q --target test .)
```

```-e DLV_LISTEN_NSMGR=:50000``` tells docker to set the environment variable DLV_LISTEN_NSMGR to :50000 telling
dlv to listen on port 50000.

```-p 50000:50000``` tells docker to forward port 50000 in the container to port 50000 in the host.  From there, you can
just connect dlv using your favorite IDE and debug nsc.

## Debugging the tests and the nsc

```bash
docker run --rm -p 40000:40000 $(docker build -q --target debug .)
```

Please note, the tests **start** the nsmgr, so until you connect to port 40000 with your debugger and walk the tests
through to the point of running nsmgr, you will not be able to attach a debugger on port 50000 to the nsmgr.
