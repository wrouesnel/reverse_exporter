FROM golang:1.10 as build

RUN mkdir -p /go/src/github.com/wrouesnel/reverse_exporter
WORKDIR /go/src/github.com/wrouesnel/reverse_exporter
COPY . .

RUN go run mage.go binary

ENTRYPOINT [ "./reverse_exporter" ]
