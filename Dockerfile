# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/upstream ./cmd/upstream

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/gateway /usr/local/bin/gateway
COPY --from=build /out/upstream /usr/local/bin/upstream
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gateway"]
CMD ["--config", "/etc/gateway/gateway.yaml"]
