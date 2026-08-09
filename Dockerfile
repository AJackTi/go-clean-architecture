# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
    -o /out/app ./cmd/app

FROM gcr.io/distroless/static-debian13:nonroot AS runtime

WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /src/config /app/config
COPY --from=build /src/db/migrations /app/db/migrations

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/app"]
