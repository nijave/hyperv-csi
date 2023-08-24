ARG BASE_IMAGE=docker.io/library/golang:1.21-bookworm

FROM $BASE_IMAGE as builder
COPY . /src
WORKDIR /src
RUN --mount=type=cache,target=/root/.cache/go-build go build .

FROM $BASE_IMAGE
RUN apt update \
    && apt install --no-install-recommends -y e2fsprogs mount parted util-linux xfsprogs \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /src/hyperv-csi /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/hyperv-csi"]