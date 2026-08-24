# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

# Keep the image tag and digest synchronized when upgrading the Go toolchain.
FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN set -eux; \
	CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build \
		-trimpath -buildvcs=false -ldflags="-s -w" -o /out/app ./cmd/app; \
	CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build \
		-trimpath -buildvcs=false -ldflags="-s -w" -o /out/migrate ./cmd/migrate; \
	CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build \
		-trimpath -buildvcs=false -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS runtime

ARG VCS_REF=unknown
ARG VERSION=dev

WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /out/migrate /app/migrate
COPY --from=build /out/healthcheck /app/healthcheck
COPY --from=build /src/db/migrations /app/db/migrations
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENV GIN_MODE=release

LABEL org.opencontainers.image.title="go-clean-architecture" \
	org.opencontainers.image.description="A production-ready Go clean architecture template" \
	org.opencontainers.image.source="https://github.com/AJackTi/go-clean-architecture" \
	org.opencontainers.image.revision="${VCS_REF}" \
	org.opencontainers.image.version="${VERSION}" \
	org.opencontainers.image.licenses="MIT"

EXPOSE 8080
STOPSIGNAL SIGTERM
USER nonroot:nonroot
ENTRYPOINT ["/app/app"]
