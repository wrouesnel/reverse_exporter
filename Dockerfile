# Dockerfile for building the containerized poller_exporter
# golang:1.18 as of 2022-07-04
FROM golang@sha256:1bbb02af44e5324a6eabe502b6a928d368977225c0255bc9aca4a734145f86e1 AS build

MAINTAINER William Rouesnel <wrouesnel@wrouesnel.com>
EXPOSE 9115

COPY ./ /workdir/
WORKDIR /workdir

RUN go run mage.go binary

FROM scratch

MAINTAINER Will Rouesnel <wrouesnel@wrouesnel.com>

ENV PATH=/bin
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs
COPY --from=build /workdir/reverse_exporter /bin/reverse_exporter
COPY pkg/config/reverse_exporter.yml /config/reverse_exporter.yml

ENTRYPOINT ["/bin/reverse_exporter"]
CMD ["--log-format=json"]
