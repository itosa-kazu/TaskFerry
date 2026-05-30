# syntax=docker/dockerfile:1

FROM golang:1.22-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGET=relay
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/taskferry ./cmd/${TARGET}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/taskferry /taskferry
USER nonroot:nonroot
ENTRYPOINT ["/taskferry"]
