FROM golang:1.16-buster as go
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOBIN=/bin
RUN go get github.com/go-delve/delve/cmd/dlv@v1.6.0
ADD https://github.com/spiffe/spire/releases/download/v0.11.1/spire-0.11.1-linux-x86_64-glibc.tar.gz .
RUN tar xzvf spire-0.11.1-linux-x86_64-glibc.tar.gz -C /bin --strip=3 ./spire-0.11.1/bin/spire-server ./spire-0.11.1/bin/spire-agent
ADD https://github.com/coredns/coredns/releases/download/v1.9.2/coredns_1.9.2_linux_amd64.tgz .
RUN tar xzvf coredns_1.9.2_linux_amd64.tgz -C /bin


FROM go as build
WORKDIR /build
COPY go.mod go.sum ./
COPY ./internal/imports imports
RUN go build ./imports
COPY . .
RUN go build -o /bin/app .

FROM build as test
CMD go test -test.v ./...

FROM test as debug
CMD dlv -l :40000 --headless=true --api-version=2 test -test.v ./...

FROM alpine as runtime
COPY --from=build /bin/app /bin/app
COPY --from=build /bin/coredns /bin/coredns

ENTRYPOINT ["/bin/app"]