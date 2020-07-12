# build cdi-driver
FROM golang:1.14 AS build

ARG GO_VERSION="1.14.4"

ARG VERSION="unknown"
ADD . /src/cdi
WORKDIR /src/cdi

# If "docker build" is invoked with the "vendor" directory correctly
# populated, then this argument can be set to -mod=vendor. "make
# build-image" does both automatically.
ARG GOFLAGS=
ARG LICENSE_DIR=/usr/local/share/package-licenses/github.com/intel/cdi

RUN mkdir -p /usr/local/share/package-licenses && \
    hack/copy-modules-license.sh /usr/local/share/package-licenses ./cmd/cdi-driver && \
    mkdir -p $LICENSE_DIR && cp LICENSE $LICENSE_DIR

RUN make VERSION=${VERSION} OUTPUT_DIR=/usr/local/bin cdi-driver

# create final cdi-driver image
FROM gcr.io/distroless/static
COPY --from=build /usr/local/bin/cdi-* /usr/local/bin/
COPY --from=build /usr/local/share/package-licenses /usr/local/share/package-licenses
