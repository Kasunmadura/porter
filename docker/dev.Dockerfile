# Development environment
# -----------------------
FROM golang:1.15-alpine
WORKDIR /porter

RUN apk update && apk add --no-cache gcc musl-dev git

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN go build -ldflags '-w -s' -a -o ./bin/app ./cmd/app \
    && chmod +x /porter/docker/bin/*

# TODO -- WILL HAVE MIGRATION
# RUN go build -ldflags '-w -s' -a -o ./bin/migrate ./cmd/migrate

EXPOSE 8080