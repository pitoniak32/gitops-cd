# github-actions-token minter (ADR 0020): static binary on scratch, plus the CA bundle for api.github.com.
# `gcd infra` builds it and loads it into the hub kind cluster; manifests/minter.yaml pins the tag (a hash
# of this directory's sources, checked by `gcd test`).
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /minter .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /minter /minter
USER 65534:65534
ENTRYPOINT ["/minter"]
