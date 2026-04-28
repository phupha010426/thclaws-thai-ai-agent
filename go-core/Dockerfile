FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/thaiagent-core ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/thaiagent-core /thaiagent-core
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/thaiagent-core"]
