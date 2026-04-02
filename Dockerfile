FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /bin/load-tester ./cmd/server

FROM --platform=$TARGETPLATFORM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /bin/load-tester /bin/load-tester
COPY --from=builder /app/docs /app/docs
COPY --from=builder /app/examples /app/examples
COPY --from=builder /app/fixtures /app/fixtures

EXPOSE 8080

ENTRYPOINT ["/bin/load-tester"]

