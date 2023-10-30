Go-Clean-Architecture

# Prerequisites

Need to install these tools first:

1. Docker and docker-compose
2. Golang v1.18+
3. Golang-migrate tool `brew install golang-migrate`
4. Golangci-lint tool `brew install golangci-lint`
5. Hadolint tool `brew install hadolint`
6. dotenv-linter tool `brew install dotenv-linter`
7. (optional) If you're using Colima instead of Docker Desktop, you need to export `DOCKER_HOST` in order to run test from _usecase_ package

# How to run

- Copy .env.example to .env
- Run `make run `

# How to run test

- Test 1 user claims multiple times concurrently
  `go test -v ./internal/usecase/.`

- Test multiple user claims at the same time
  `go test -v ./internal/entity/.`

# Problems need to solve

# Current file size after build

We need to track and check how we can reduce this size

# Deployment diagram
