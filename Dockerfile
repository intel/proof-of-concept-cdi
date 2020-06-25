# CLEARLINUX_BASE and SWUPD_UPDATE_ARG can be used to make the build reproducible
# by choosing an image by its hash and updating to a certain version with -V:
# CLEAR_LINUX_BASE=clearlinux@sha256:b8e5d3b2576eb6d868f8d52e401f678c873264d349e469637f98ee2adf7b33d4
# SWUPD_UPDATE_ARG=-V 29970
#
# This is used on release branches before tagging a stable version. The master and devel
# branches default to using the latest Clear Linux.
ARG CLEAR_LINUX_BASE=clearlinux:latest
ARG SWUPD_UPDATE_ARG=

# Common base image for building PMEM-CSI:
# - up-to-date Clear Linux
FROM ${CLEAR_LINUX_BASE} AS build
ARG CLEAR_LINUX_BASE
ARG SWUPD_UPDATE_ARG

ARG GO_VERSION="1.13.4"

#pull dependencies required for downloading
ARG CACHEBUST
RUN echo "Updating build image from ${CLEAR_LINUX_BASE} to ${SWUPD_UPDATE_ARG:-the latest release}."
RUN swupd update ${SWUPD_UPDATE_ARG} && swupd bundle-add c-basic curl && rm -rf /var/lib/swupd /var/tmp/swupd
RUN curl -L https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz | tar -zxf - -C / && \
    mkdir -p /usr/local/bin/ && \
    for i in /go/bin/*; do ln -s $i /usr/local/bin/; done

# Clean image for deploying CDI.
FROM ${CLEAR_LINUX_BASE} as runtime
ARG CLEAR_LINUX_BASE
ARG SWUPD_UPDATE_ARG
ARG BIN_SUFFIX
LABEL maintainers="Intel"
LABEL description="CDI Driver"

# update and install needed bundles:
# file - driver uses file utility to determine filesystem type
# xfsprogs - XFS filesystem utilities
# storge-utils - for lvm2 and ext4(e2fsprogs) utilities
ARG CACHEBUST
RUN echo "Updating runtime image from ${CLEAR_LINUX_BASE} to ${SWUPD_UPDATE_ARG:-the latest release}."
RUN swupd update ${SWUPD_UPDATE_ARG} && swupd bundle-add file xfsprogs storage-utils \
    $(if [ "$BIN_SUFFIX" = "-test" ]; then echo fio; fi) && \
    rm -rf /var/lib/swupd /var/tmp/swupd

# Image in which CDI binaries get built.
FROM build as binaries

# build cdi-driver
ARG VERSION="unknown"
ADD . /src/cdi
ENV PKG_CONFIG_PATH=/usr/lib/pkgconfig/
WORKDIR /src/cdi
ARG BIN_SUFFIX

# If "docker build" is invoked with the "vendor" directory correctly
# populated, then this argument can be set to -mod=vendor. "make
# build-images" does both automatically.
ARG GOFLAGS=

# Here we choose explicitly which binaries we want in the image and in
# which flavor (production or testing). The actual binary name in the
# image is going to be the same, to avoid unnecessary deployment
# differences.
RUN set -x && \
    make VERSION=${VERSION} cdi-driver${BIN_SUFFIX} && \
    mkdir -p /usr/local/bin && \
    mv _output/cdi-driver${BIN_SUFFIX} /usr/local/bin/cdi-driver && \
    mkdir -p /usr/local/share/package-licenses && \
    hack/copy-modules-license.sh /usr/local/share/package-licenses ./cmd/cdi-driver && \
    cp /go/LICENSE /usr/local/share/package-licenses/go.LICENSE && \
    cp LICENSE /usr/local/share/package-licenses/CDI.LICENSE

# The actual cdi-driver image.
FROM runtime as cdi

# Move required binaries and libraries to clean container.
# All of our custom content is in /usr/local.
RUN for i in /usr/local/lib/lib*.so.*; do ln -fs $i /usr/lib64; done
COPY --from=binaries /usr/local/bin/cdi-* /usr/local/bin/
COPY --from=binaries /usr/local/share/package-licenses /usr/local/share/package-licenses

ENV LD_LIBRARY_PATH=/usr/lib
# By default container runs with non-root user
# Choose root user explicitly only where needed, like - node driver
RUN useradd --uid 1000 --user-group --shell /bin/bash cdi
USER 1000
