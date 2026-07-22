FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proxy-controller ./cmd/controller

FROM alpine:3.23
RUN addgroup -S controller && adduser -S -G controller controller
COPY --from=build /out/proxy-controller /proxy-controller
USER controller
ENTRYPOINT ["/proxy-controller"]
