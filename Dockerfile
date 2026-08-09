# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

# Keep the image tag and digest synchronized when upgrading the Go toolchain.
FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

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

FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6 AS runtime

ARG VCS_REF=unknown

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
	org.opencontainers.image.revision="${VCS_REF}"

EXPOSE 8080
STOPSIGNAL SIGTERM
USER nonroot:nonroot
ENTRYPOINT ["/app/app"]
