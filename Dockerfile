FROM cgr.dev/chainguard/go AS builder

WORKDIR /build
ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto
COPY ./cmd/ ./cmd/
COPY ./internal/ ./internal/
COPY ./testfiles/ ./testfiles/
COPY ./Makefile .
COPY ./go.mod .
COPY ./go.sum .
RUN make build
RUN make test

FROM cgr.dev/chainguard/static
WORKDIR /app
COPY --from=builder /build/bin/whodis .
CMD ["/app/whodis"]
