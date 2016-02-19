FROM gliderlabs/alpine:3.2
ENTRYPOINT ["/bin/registrator"]

COPY . /go/src/github.com/xytis/registrator
RUN apk-install -t build-deps go git mercurial \
	&& cd /go/src/github.com/xytis/registrator \
	&& export GOPATH=/go \
	&& go get \
	&& go build -ldflags "-X main.Version $(cat VERSION)" -o /bin/registrator \
	&& rm -rf /go \
	&& apk del --purge build-deps
