ARG BASE_IMAGE=docker.io/library/golang:1.21-bookworm

FROM $BASE_IMAGE as builder
COPY . /src
WORKDIR /src
RUN --mount=type=cache,target=/root/.cache/go-build go build .

FROM $BASE_IMAGE
COPY --from=builder /src/hyperv-csi /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/hyperv-csi"]