# promotion-log (ADR 0032): static binary on scratch. `gcd infra` builds it and loads it into the hub kind
# cluster; manifests/promotion-log.yaml pins the tag (a hash of this directory's sources, checked by `gcd test`).
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /promotion-log .

FROM scratch
COPY --from=build /promotion-log /promotion-log
USER 65534:65534
ENTRYPOINT ["/promotion-log"]
