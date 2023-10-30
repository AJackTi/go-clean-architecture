# Step 1: Modules caching
FROM golang:1.18.3-alpine3.16 as modules
COPY go.mod go.sum /modules/
WORKDIR /modules
RUN go mod download

# Step 2: Builder
FROM golang:1.18.3-alpine3.16 as builder
COPY --from=modules /go/pkg /go/pkg
COPY . /app
WORKDIR /app
RUN apk update && apk add libc6-compat gcc g++
RUN GOOS=linux GOARCH=amd64 \
    go build -tags migrate -o /bin/app ./cmd/app

# Step 3: Final
#FROM scratch
#COPY --from=builder /app/config /config
#COPY --from=builder /app/migrations /migrations
#COPY --from=builder /bin/app /app
#COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
FROM builder AS app
CMD ["/bin/app"]

# FROM builder AS bot
# CMD ["/bin/app", "bot"]
